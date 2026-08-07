package app

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters follow the OWASP recommendation for interactive login.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// HashPassword returns a self-describing PHC string:
// $argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

// VerifyPassword re-derives the key with the parameters embedded in the
// stored string, so parameter upgrades never invalidate old hashes.
func VerifyPassword(stored, password string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false
	}
	var mem, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, mem, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// burnPasswordTime spends what a real password check costs, for the paths that
// never reach one.
//
// Without it this login leaks who works here. An unknown address returns after
// a missed index lookup — under a millisecond — while a known one runs argon2id
// over 64 MiB first. That gap is not subtle; it is two orders of magnitude, and
// it turns "is there an account at this address" into a question anybody can
// ask by timing the answer. Which matters more here than it did under
// usernames, because company addresses are guessable: 名字@公司域名.
//
// The decoy is hashed once at startup from bytes nobody has, so the comparison
// always fails and always costs the same as the real thing.
var decoyHash = func() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Keeping a fixed fallback rather than failing: a process that will
		// not start is worse than a decoy an attacker cannot reach anyway.
		b = []byte("iam-decoy-fallback")
	}
	h, err := HashPassword(base64.RawStdEncoding.EncodeToString(b))
	if err != nil {
		return ""
	}
	return h
}()

func burnPasswordTime() {
	if decoyHash == "" {
		return
	}
	_ = VerifyPassword(decoyHash, "not-the-password")
}
