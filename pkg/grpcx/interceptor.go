package grpcx

import (
	"context"
	"log/slog"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
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
//
// Reading the signing key here rather than at first request is deliberate: a
// service missing it dies on start, with one readable line, instead of
// answering every call with an error nobody can place. See SigningKey.
func ServerInterceptors(log *slog.Logger) grpc.ServerOption {
	_ = SigningKey()
	return grpc.ChainUnaryInterceptor(
		unaryRecovery(log),
		unaryOperator(log),
		unaryLog(log),
		unaryError(log),
	)
}

// unaryOperator restores the operator from incoming metadata — and refuses
// the call unless the metadata was signed by something holding our key.
//
// The identity used to be taken on trust. x-employee-id: 1 arrived as a plain
// header and was read straight into this struct, so anything that could reach
// the port was the administrator of every tenant. The verification below is
// what makes the claims below it worth anything.
func unaryOperator(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		op := Operator{
			TenantID:   firstInt(md, mdTenantID),
			EmployeeID: firstInt(md, mdEmployeeID),
			Name:       decodeHeader(first(md, mdEmployeeName)),
			IP:         first(md, mdIP),
			TraceID:    first(md, mdTraceID),
		}
		// Verified against the claims exactly as they arrived, before the
		// tenant default below rewrites one of them — the signature covers
		// what the caller sent, not what we decided to make of it.
		if err := verify(md, op, info.FullMethod, time.Now()); err != nil {
			// The reason goes to the log, never to the caller. To them every
			// rejection looks the same, because "your clock is off" and "your
			// key is wrong" are useful to somebody probing and to nobody else.
			log.Warn("refused an unsigned or badly signed call",
				"method", info.FullMethod, "reason", err.Error())
			return nil, status.Error(codes.Unauthenticated, "unauthenticated")
		}
		if op.TenantID == 0 {
			// 「没有登录用户就算第一家公司」。这句默认值已经咬过一次：提单
			// 提醒的后台任务在没有登录用户的上下文里问「物流部都有谁」，拿
			// 回的是第一家公司的人，然后发给了每一家公司（见 shipping/
			// blreminder.go 的修复）。后台任务必须用 WithOperator 显式带上
			// 它正在处理的那家公司。
			//
			// 默认值本身先不拆：登录、激活、健康检查这些调用合法地没有登录
			// 态，贸然改成拒绝会砸掉它们。改成拆除的条件写在这里——生产日志
			// 里这条 WARN 在合法名单之外安静了，就可以把默认改成拒绝。
			if !tenantlessAllowed(info.FullMethod) {
				log.Warn("grpcx: a call with no tenant was defaulted to tenant 1 — "+
					"background work must attach its tenant with WithOperator",
					"method", info.FullMethod)
			}
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
		// The operator is optional; the signature is not.
		//
		// Background work — mailbox sync, the image cache, the backfills —
		// calls other services with nobody logged in. Signing only when an
		// operator is present would have left every one of those paths
		// unauthenticated, which is precisely the half worth attacking.
		op, _ := OperatorFromContext(ctx)
		if op.TenantID != 0 || op.EmployeeID != 0 || op.Name != "" {
			ctx = metadata.AppendToOutgoingContext(ctx,
				mdTenantID, strconv.FormatInt(op.TenantID, 10),
				mdEmployeeID, strconv.FormatInt(op.EmployeeID, 10),
				mdEmployeeName, url.QueryEscape(op.Name),
				mdTraceID, op.TraceID,
			)
		}
		ts := time.Now().Unix()
		ctx = metadata.AppendToOutgoingContext(ctx,
			mdTimestamp, strconv.FormatInt(ts, 10),
			mdSignature, sign(op, ts, method),
		)
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

// tenantlessAllowed lists the calls that legitimately arrive with nobody
// logged in. Everything else that shows up without a tenant is a background
// task that forgot WithOperator — and is about to operate on the first
// company's data by accident.
func tenantlessAllowed(method string) bool {
	return strings.HasPrefix(method, "/erp.iam.v1.AuthService/") ||
		strings.HasPrefix(method, "/grpc.health.")
}
