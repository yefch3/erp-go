package authtoken

import (
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	tok, err := Issue("secret", time.Hour, 1, 42, "张三")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := Parse("secret", tok)
	if err != nil {
		t.Fatal(err)
	}
	if claims.TenantID != 1 || claims.EmployeeID() != 42 || claims.EmployeeName != "张三" {
		t.Fatalf("claims mismatch: %+v", claims)
	}
}

func TestWrongSecretRejected(t *testing.T) {
	tok, _ := Issue("secret", time.Hour, 1, 42, "x")
	if _, err := Parse("other", tok); err == nil {
		t.Fatal("token verified with wrong secret")
	}
}

func TestExpiredRejected(t *testing.T) {
	tok, _ := Issue("secret", -time.Minute, 1, 42, "x")
	if _, err := Parse("secret", tok); err == nil {
		t.Fatal("expired token accepted")
	}
}
