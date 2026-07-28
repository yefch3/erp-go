// Package apierr defines the business error model shared by every service.
// Domain and application layers return *Error; the gRPC error interceptor
// maps it onto google.rpc.Status so callers can branch on a stable code
// instead of matching message strings.
package apierr

import (
	"errors"
	"fmt"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Kind decides the gRPC (and eventually HTTP) mapping of an error.
type Kind int

const (
	KindInvalid      Kind = iota // bad input                  -> InvalidArgument / 400
	KindNotFound                 // missing resource           -> NotFound / 404
	KindConflict                 // state or uniqueness clash  -> FailedPrecondition / 409
	KindPermission               // authenticated but not allowed -> PermissionDenied / 403
	KindUnauthorized             // not authenticated          -> Unauthenticated / 401
	KindInternal                 // everything else            -> Internal / 500
)

// Error is a business error with a stable machine-readable code such as
// "EXPORT_QTY_EXCEEDS_CONTRACT". Codes are SCREAMING_SNAKE, prefixed by
// module, and never reused for a different meaning.
type Error struct {
	Kind Kind
	Code string
	Msg  string
	// Meta carries structured context (ids, limits) for the client.
	Meta map[string]string
	err  error
}

func (e *Error) Error() string {
	if e.err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Msg, e.err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Msg)
}

func (e *Error) Unwrap() error { return e.err }

// Wrap attaches an underlying cause while keeping the business identity.
func (e *Error) Wrap(err error) *Error {
	clone := *e
	clone.err = err
	return &clone
}

// WithMeta returns a copy carrying extra structured context.
func (e *Error) WithMeta(kv ...string) *Error {
	clone := *e
	clone.Meta = map[string]string{}
	for k, v := range e.Meta {
		clone.Meta[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		clone.Meta[kv[i]] = kv[i+1]
	}
	return &clone
}

func New(kind Kind, code, msg string) *Error {
	return &Error{Kind: kind, Code: code, Msg: msg}
}

func Invalid(code, msg string) *Error      { return New(KindInvalid, code, msg) }
func NotFound(code, msg string) *Error     { return New(KindNotFound, code, msg) }
func Conflict(code, msg string) *Error     { return New(KindConflict, code, msg) }
func Permission(code, msg string) *Error   { return New(KindPermission, code, msg) }
func Unauthorized(code, msg string) *Error { return New(KindUnauthorized, code, msg) }
func Internal(code, msg string) *Error     { return New(KindInternal, code, msg) }

var kindToGRPC = map[Kind]codes.Code{
	KindInvalid:      codes.InvalidArgument,
	KindNotFound:     codes.NotFound,
	KindConflict:     codes.FailedPrecondition,
	KindPermission:   codes.PermissionDenied,
	KindUnauthorized: codes.Unauthenticated,
	KindInternal:     codes.Internal,
}

// ToStatus converts any error into a gRPC status. Unknown errors become
// Internal with a generic message: internals must not leak to clients.
func ToStatus(err error) error {
	if err == nil {
		return nil
	}
	var e *Error
	if !errors.As(err, &e) {
		return status.Error(codes.Internal, "internal error")
	}
	st := status.New(kindToGRPC[e.Kind], e.Msg)
	info := &errdetails.ErrorInfo{Reason: e.Code, Domain: "erp", Metadata: e.Meta}
	if withDetails, derr := st.WithDetails(info); derr == nil {
		st = withDetails
	}
	return st.Err()
}

// CodeFromError extracts the business code from an error that has not
// crossed a gRPC boundary, e.g. inside an event consumer calling its own
// application service. Returns "" for anything that is not a business error.
func CodeFromError(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// CodeFromStatus extracts the business code from a gRPC error on the
// caller side. Returns "" when the error carries no ErrorInfo detail.
func CodeFromStatus(err error) string {
	st, ok := status.FromError(err)
	if !ok {
		return ""
	}
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info.Reason
		}
	}
	return ""
}
