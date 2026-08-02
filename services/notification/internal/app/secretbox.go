package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// SecretBox encrypts mailbox credentials at rest.
//
// AES-256-GCM, key from the environment, nonce prepended to the ciphertext.
// GCM rather than CBC because it authenticates: a credential blob that has
// been tampered with must fail loudly rather than decrypt to rubbish that we
// then send to a mail host as somebody's password.
//
// The key never has a default. A service that cannot find its key refuses to
// start, because the alternative — quietly falling back to a built-in
// constant — produces a system that looks encrypted and is not.
type SecretBox struct {
	aead    cipher.AEAD
	version int
}

// ErrNoKey is returned when the environment carries no encryption key. The
// caller decides whether that is fatal; for anything that touches mail
// accounts it must be.
var ErrNoKey = errors.New("mail credential key is not set")

// NewSecretBox builds a box from a base64-encoded 32-byte key.
func NewSecretBox(keyB64 string, version int) (*SecretBox, error) {
	if keyB64 == "" {
		return nil, ErrNoKey
	}
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, fmt.Errorf("mail credential key is not valid base64: %w", err)
	}
	if len(key) != 32 {
		// Said with the actual length because the usual mistake is pasting a
		// hex string or a passphrase, and "invalid key size" alone sends
		// people looking in the wrong place.
		return nil, fmt.Errorf("mail credential key must decode to 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: aead, version: version}, nil
}

// Version identifies the key in use, so a rotation can leave old rows
// readable instead of requiring every employee to re-enter their code at once.
func (b *SecretBox) Version() int { return b.version }

// Seal encrypts plaintext. aad binds the ciphertext to the row it belongs to:
// a blob lifted from one employee's row and pasted into another's will not
// decrypt, so a database write cannot be used to send mail as somebody else.
func (b *SecretBox) Seal(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plaintext, aad), nil
}

// Open decrypts. The error is deliberately vague: a caller distinguishing
// "wrong key" from "tampered" from "truncated" gives an attacker with write
// access to the table an oracle, and none of the three is separately
// actionable for us anyway.
func (b *SecretBox) Open(blob, aad []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(blob) < n {
		return nil, errors.New("stored credential is unreadable")
	}
	out, err := b.aead.Open(nil, blob[:n], blob[n:], aad)
	if err != nil {
		return nil, errors.New("stored credential is unreadable")
	}
	return out, nil
}

// AccountAAD is the additional data bound into every credential blob.
//
// It is not secret — it is the row's identity. Including the tenant as well
// as the account means a blob is useless outside the exact row it was written
// for, even in a multi-tenant database somebody has direct access to.
func AccountAAD(tenantID, accountID int64) []byte {
	return []byte(fmt.Sprintf("mail_account:%d:%d", tenantID, accountID))
}
