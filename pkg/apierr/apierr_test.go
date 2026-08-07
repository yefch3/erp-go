package apierr

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestToStatusMapsKind(t *testing.T) {
	cases := []struct {
		kind Kind
		want codes.Code
	}{
		{KindInvalid, codes.InvalidArgument},
		{KindNotFound, codes.NotFound},
		{KindConflict, codes.FailedPrecondition},
		{KindPermission, codes.PermissionDenied},
		{KindUnauthorized, codes.Unauthenticated},
		{KindInternal, codes.Internal},
		{KindThrottled, codes.ResourceExhausted},
	}
	for _, c := range cases {
		err := ToStatus(New(c.kind, "X_CODE", "boom"))
		if got := status.Code(err); got != c.want {
			t.Fatalf("kind %d mapped to %s, want %s", c.kind, got, c.want)
		}
	}
}

func TestBusinessCodeSurvivesTheWire(t *testing.T) {
	orig := Conflict("EXPORT_QTY_EXCEEDS_CONTRACT", "cumulative qty exceeds contract").
		WithMeta("contract_id", "42")
	err := ToStatus(fmt.Errorf("usecase: %w", orig))
	if got := CodeFromStatus(err); got != "EXPORT_QTY_EXCEEDS_CONTRACT" {
		t.Fatalf("code = %q, want EXPORT_QTY_EXCEEDS_CONTRACT", got)
	}
}

func TestUnknownErrorsDoNotLeak(t *testing.T) {
	err := ToStatus(errors.New("pq: password authentication failed for user"))
	st, _ := status.FromError(err)
	if st.Message() != "internal error" {
		t.Fatalf("leaked internal message: %q", st.Message())
	}
	if st.Code() != codes.Internal {
		t.Fatalf("code = %s, want Internal", st.Code())
	}
}

// A kind that maps to Internal is a kind nobody wired up: the caller sees a
// 500 and a generic message instead of whatever the service actually said.
// Adding one is easy to do and invisible until somebody hits that path, so
// this asserts on every kind that exists rather than on a list to remember to
// update.
func TestEveryKindHasAMapping(t *testing.T) {
	for kind := KindInvalid; kind <= KindThrottled; kind++ {
		if _, ok := kindToGRPC[kind]; !ok {
			t.Fatalf("kind %d has no gRPC mapping, so it would surface as Internal", kind)
		}
	}
}

// "Later" must not read as "no". The gateway charges a failed-attempt budget
// on Unauthenticated, so a throttle wearing that code would spend the very
// budget the waiting exists to restore — and an outage in one service would
// lock people out of another.
func TestAThrottleIsNotAnAuthenticationFailure(t *testing.T) {
	if kindToGRPC[KindThrottled] == kindToGRPC[KindUnauthorized] {
		t.Fatal("a throttle is indistinguishable from a rejected credential on the wire")
	}
}
