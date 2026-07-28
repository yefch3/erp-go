package grpcx

import (
	"context"
	"log/slog"
	"net/url"
	"runtime/debug"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/sgao19/erp-go/pkg/apierr"
)

const (
	mdTenantID     = "x-tenant-id"
	mdEmployeeID   = "x-employee-id"
	mdEmployeeName = "x-employee-name"
	mdIP           = "x-forwarded-for"
	mdTraceID      = "x-trace-id"
)

// ServerInterceptors is the standard chain for every service, in order:
// recovery (outermost), operator context, request log, error mapping.
func ServerInterceptors(log *slog.Logger) grpc.ServerOption {
	return grpc.ChainUnaryInterceptor(
		unaryRecovery(log),
		unaryOperator(),
		unaryLog(log),
		unaryError(log),
	)
}

// unaryOperator restores the operator from incoming metadata.
func unaryOperator() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		op := Operator{
			TenantID:   firstInt(md, mdTenantID),
			EmployeeID: firstInt(md, mdEmployeeID),
			Name:       decodeHeader(first(md, mdEmployeeName)),
			IP:         first(md, mdIP),
			TraceID:    first(md, mdTraceID),
		}
		if op.TenantID == 0 {
			op.TenantID = 1
		}
		return handler(WithOperator(ctx, op), req)
	}
}

// unaryError maps domain errors (apierr.Error) onto gRPC statuses with a
// stable business code, so clients never match on message strings.
func unaryError(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)
		if err == nil {
			return resp, nil
		}
		if _, ok := status.FromError(err); ok && apierr.CodeFromStatus(err) != "" {
			return nil, err // already a mapped status
		}
		// Anything that is not a business error becomes a bare "internal
		// error" for the caller, on purpose: internals must not leak. But it
		// has to be readable on OUR side, or an outage is undiagnosable —
		// log the cause here, where it still exists.
		log.ErrorContext(ctx, "unhandled error", "method", info.FullMethod, "err", err.Error())
		return nil, apierr.ToStatus(err)
	}
}

func unaryLog(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		op, _ := OperatorFromContext(ctx)
		attrs := []any{
			"method", info.FullMethod,
			"duration_ms", time.Since(start).Milliseconds(),
			"tenant_id", op.TenantID,
			"employee_id", op.EmployeeID,
			"trace_id", op.TraceID,
		}
		if err != nil {
			attrs = append(attrs, "code", status.Code(err).String(),
				"biz_code", apierr.CodeFromStatus(err), "err", err.Error())
			log.ErrorContext(ctx, "rpc", attrs...)
		} else {
			log.InfoContext(ctx, "rpc", attrs...)
		}
		return resp, err
	}
}

func unaryRecovery(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.ErrorContext(ctx, "rpc panic",
					"method", info.FullMethod, "panic", r, "stack", string(debug.Stack()))
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// UnaryClientPropagator forwards the operator metadata on outgoing calls so
// downstream services see the same actor and trace.
func UnaryClientPropagator() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any,
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if op, ok := OperatorFromContext(ctx); ok {
			ctx = metadata.AppendToOutgoingContext(ctx,
				mdTenantID, strconv.FormatInt(op.TenantID, 10),
				mdEmployeeID, strconv.FormatInt(op.EmployeeID, 10),
				mdEmployeeName, url.QueryEscape(op.Name),
				mdTraceID, op.TraceID,
			)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func first(md metadata.MD, key string) string {
	if vs := md.Get(key); len(vs) > 0 {
		return vs[0]
	}
	return ""
}

func firstInt(md metadata.MD, key string) int64 {
	n, _ := strconv.ParseInt(first(md, key), 10, 64)
	return n
}

// decodeHeader reverses the percent-encoding the client propagator applies:
// gRPC metadata values must be printable ASCII, so non-ASCII operator names
// (员工姓名) travel URL-encoded.
func decodeHeader(v string) string {
	if decoded, err := url.QueryUnescape(v); err == nil {
		return decoded
	}
	return v
}
