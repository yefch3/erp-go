package authtoken

import (
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	tok, err := Issue("secret", time.Hour, 1, 42, "张三", "zhangsan@thecompany.com")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := Parse("secret", tok)
	if err != nil {
		t.Fatal(err)
	}
	if claims.TenantID != 1 || claims.EmployeeID() != 42 || claims.EmployeeName != "张三" ||
		claims.Email != "zhangsan@thecompany.com" {
		t.Fatalf("claims mismatch: %+v", claims)
	}
}

func TestWrongSecretRejected(t *testing.T) {
	tok, _ := Issue("secret", time.Hour, 1, 42, "x", "x@y.com")
	if _, err := Parse("other", tok); err == nil {
		t.Fatal("token verified with wrong secret")
	}
}

func TestExpiredRejected(t *testing.T) {
	tok, _ := Issue("secret", -time.Minute, 1, 42, "x", "x@y.com")
	if _, err := Parse("secret", tok); err == nil {
		t.Fatal("expired token accepted")
	}
}

// The address has to survive the round trip, because the mailbox gate binds
// what the token says rather than what a caller sends. A claim that came back
// empty would mean nobody could bind a mailbox at all.
func TestTheLoginAddressSurvivesTheRoundTrip(t *testing.T) {
	tok, err := Issue("secret", time.Hour, 7, 99, "Alice", "Alice@TheCompany.com")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := Parse("secret", tok)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Email != "Alice@TheCompany.com" {
		t.Fatalf("email claim = %q, want it carried through unchanged", claims.Email)
	}
}
