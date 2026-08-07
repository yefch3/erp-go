package app

import (
	"strings"
	"testing"
)

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("s3cret-密码")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}
	if !VerifyPassword(hash, "s3cret-密码") {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("wrong password accepted")
	}
}

func TestHashesAreSalted(t *testing.T) {
	h1, _ := HashPassword("same")
	h2, _ := HashPassword("same")
	if h1 == h2 {
		t.Fatal("two hashes of the same password are identical: salt missing")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"", "plaintext", "$argon2id$v=19$broken", "$md5$x$y$z$w"} {
		if VerifyPassword(bad, "anything") {
			t.Fatalf("malformed hash %q accepted", bad)
		}
	}
}
