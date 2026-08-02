package app

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func testKey(t *testing.T) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
}

func TestSealAndOpenRoundTrips(t *testing.T) {
	box, err := NewSecretBox(testKey(t), 1)
	if err != nil {
		t.Fatalf("NewSecretBox: %v", err)
	}
	aad := AccountAAD(1, 42)
	secret := []byte("263-auth-code-abc123")

	blob, err := box.Seal(secret, aad)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Contains(blob, secret) {
		t.Fatal("ciphertext contains the plaintext")
	}

	got, err := box.Open(blob, aad)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, secret) {
		t.Fatalf("round trip changed the secret: %q", got)
	}
}

// The same secret must not produce the same ciphertext twice, or the table
// would leak which employees share a password.
func TestSealIsNotDeterministic(t *testing.T) {
	box, _ := NewSecretBox(testKey(t), 1)
	aad := AccountAAD(1, 42)

	a, _ := box.Seal([]byte("same"), aad)
	b, _ := box.Seal([]byte("same"), aad)
	if bytes.Equal(a, b) {
		t.Fatal("two seals of the same plaintext produced identical ciphertext")
	}
}

// The point of binding the row identity: a blob moved to another employee's
// row must not decrypt, so write access to the table cannot be turned into
// sending mail as somebody else.
func TestBlobDoesNotOpenUnderAnotherAccount(t *testing.T) {
	box, _ := NewSecretBox(testKey(t), 1)

	blob, _ := box.Seal([]byte("lina's code"), AccountAAD(1, 42))

	if _, err := box.Open(blob, AccountAAD(1, 43)); err == nil {
		t.Fatal("blob opened under a different account id")
	}
	if _, err := box.Open(blob, AccountAAD(2, 42)); err == nil {
		t.Fatal("blob opened under a different tenant")
	}
}

func TestTamperedBlobIsRejected(t *testing.T) {
	box, _ := NewSecretBox(testKey(t), 1)
	aad := AccountAAD(1, 42)
	blob, _ := box.Seal([]byte("263-auth-code-abc123"), aad)

	tampered := bytes.Clone(blob)
	tampered[len(tampered)-1] ^= 0xff

	if _, err := box.Open(tampered, aad); err == nil {
		t.Fatal("a modified ciphertext decrypted successfully")
	}
}

func TestTruncatedBlobIsRejected(t *testing.T) {
	box, _ := NewSecretBox(testKey(t), 1)
	if _, err := box.Open([]byte{1, 2, 3}, AccountAAD(1, 42)); err == nil {
		t.Fatal("a blob shorter than the nonce decrypted successfully")
	}
}

// A missing key must be its own error so startup can refuse to run rather
// than falling back to something that only looks like encryption.
func TestMissingKeyIsDistinguishable(t *testing.T) {
	if _, err := NewSecretBox("", 1); err != ErrNoKey {
		t.Fatalf("want ErrNoKey, got %v", err)
	}
}

func TestWrongSizedKeyIsRejectedWithItsLength(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 16))
	_, err := NewSecretBox(short, 1)
	if err == nil {
		t.Fatal("a 16-byte key was accepted")
	}
	// The usual mistake is pasting a passphrase or a hex string; naming the
	// actual length is what stops people looking in the wrong place.
	if !strings.Contains(err.Error(), "16") {
		t.Fatalf("error should state the decoded length, got: %v", err)
	}
}

func TestNonBase64KeyIsRejected(t *testing.T) {
	if _, err := NewSecretBox("not base64!!", 1); err == nil {
		t.Fatal("a non-base64 key was accepted")
	}
}

// Whatever Open fails on, it must say the same thing: separate messages for
// "wrong key" and "tampered" hand an oracle to anyone with write access.
func TestOpenErrorsDoNotDistinguishFailureModes(t *testing.T) {
	box, _ := NewSecretBox(testKey(t), 1)
	aad := AccountAAD(1, 42)
	blob, _ := box.Seal([]byte("secret"), aad)

	other, _ := NewSecretBox(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)), 1)
	_, wrongKey := other.Open(blob, aad)

	tampered := bytes.Clone(blob)
	tampered[len(tampered)-1] ^= 0xff
	_, modified := box.Open(tampered, aad)

	_, wrongAAD := box.Open(blob, AccountAAD(1, 99))

	if wrongKey == nil || modified == nil || wrongAAD == nil {
		t.Fatal("expected all three to fail")
	}
	if wrongKey.Error() != modified.Error() || modified.Error() != wrongAAD.Error() {
		t.Fatalf("failure modes are distinguishable: %q / %q / %q",
			wrongKey, modified, wrongAAD)
	}
}
