// Package grpcx carries the cross-cutting gRPC plumbing every service uses:
// operator/tenant context propagation, error mapping, panic recovery and
// structured request logging.
package grpcx

import (
	"context"
)

// Operator is the authenticated actor behind a request, reconstructed from
// gRPC metadata by the server interceptor. TenantID is always present
// (single-tenant deployments run as tenant 1).
type Operator struct {
	TenantID   int64
	EmployeeID int64
	Name       string
	// The address they logged in with. The mailbox gate binds this one and
	// takes no address from the request, so there is nothing to disagree with.
	Email   string
	IP      string
	TraceID string
}

type ctxKey struct{}

func WithOperator(ctx context.Context, op Operator) context.Context {
	return context.WithValue(ctx, ctxKey{}, op)
}

// OperatorFromContext returns the operator and whether one was attached.
// Handlers that require an operator should fail Unauthenticated when absent.
func OperatorFromContext(ctx context.Context) (Operator, bool) {
	op, ok := ctx.Value(ctxKey{}).(Operator)
	return op, ok
}

// TenantID returns the tenant carried by the authenticated request.
//
// Zero deliberately means "there is no tenant". Public RPCs such as login
// are allowed to run before a tenant is known, and turning that absence into
// tenant 1 would let an accidental tenant-scoped query read the first
// company's data. Authenticated RPCs are rejected by the interceptor before
// they reach a handler when the tenant is missing.
func TenantID(ctx context.Context) int64 {
	if op, ok := OperatorFromContext(ctx); ok && op.TenantID > 0 {
		return op.TenantID
	}
	return 0
}
