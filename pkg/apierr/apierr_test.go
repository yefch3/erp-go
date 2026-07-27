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
