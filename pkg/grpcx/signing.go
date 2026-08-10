package grpcx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/metadata"
)

// Proving that a gRPC call came from one of our own services.
//
// Until this existed, a service took the caller's word for everything. The
// identity travelled as plain metadata — x-employee-id: 1 — and the receiving
// end read it into a struct without asking anything. Anyone who could open a
// TCP connection to port 9001 was the administrator of every tenant, with no
// password, and the only thing preventing that was the port being bound to
// loopback. One defence, and it was network configuration rather than code.
//
// What this adds is a second one: the caller has to know a shared secret. It
// is not a complete answer — every service holds the same key, so this proves
// "one of us" rather than "the mail service specifically" — but it turns
// "reach the port and you are root" into "reach the port *and* hold the key".
// Per-service keys or mTLS are the next step and are deliberately not this
// step; see the zero-trust section in docs/ARCHITECTURE.md.
//
// Two properties are worth stating because they are easy to lose later.
//
// **The signature authenticates the service, not the person.** Background work
// — mailbox sync, the image cache, the backfills — calls other services with
// no operator in context at all. Those calls are signed too, over an empty
// identity. Signing only when somebody is logged in would have left every
// background path unauthenticated, which is the half an attacker would pick.
//
// **The method name is signed.** Without it, a signature captured from a
// harmless call could be replayed onto a destructive one carrying the same
// claims. It does not stop a replay of the *same* call within the clock
// window, which is a smaller thing: it repeats an action rather than
// inventing one.

const (
	mdSignature = "x-erp-sig"
	mdTimestamp = "x-erp-ts"

	// How far apart the two clocks may be. These processes usually share a
	// host, so this is generous; it exists to bound how long a captured
	// signature stays useful, not to accommodate real drift.
	signatureWindow = 2 * time.Minute

	signingKeyEnv = "INTERNAL_SIGNING_KEY"
)

var (
	keyOnce sync.Once
	key     []byte
)

// SigningKey is the shared secret, read once from the environment.
//
// It panics when unset, and that is the intended behaviour: a service that
// cannot verify its callers must not start serving. The same reasoning as
// MAIL_CRED_KEY, which has never been allowed a default — a development
// fallback becomes the production value, and a signing key with a value
// anybody can read out of the repository authenticates nobody.
func SigningKey() []byte {
	keyOnce.Do(func() {
		v := strings.TrimSpace(os.Getenv(signingKeyEnv))
		if v == "" {
			panic(signingKeyEnv + " is not set. Every service needs the same value; " +
				"generate one with `openssl rand -hex 32` and put it in deploy/.env. " +
				"There is deliberately no default: see docs/DEPLOY.md.")
		}
		if len(v) < 32 {
			panic(signingKeyEnv + " is shorter than 32 characters, which is not a key.")
		}
		key = []byte(v)
	})
	return key
}

// claims is what a signature covers. Assembled the same way on both sides,
// from the same fields, in the same order — a canonical form rather than a
// map, because iteration order would make the two ends disagree at random.
func claims(op Operator, ts int64, method string) string {
	return strings.Join([]string{
		"v1",
		strconv.FormatInt(op.TenantID, 10),
		strconv.FormatInt(op.EmployeeID, 10),
		url.QueryEscape(op.Name),
		strconv.FormatInt(ts, 10),
		method,
	}, "|")
}

func sign(op Operator, ts int64, method string) string {
	mac := hmac.New(sha256.New, SigningKey())
	mac.Write([]byte(claims(op, ts, method)))
	return hex.EncodeToString(mac.Sum(nil))
}

// verify checks the signature and the clock, and says which failed — to the
// log, never to the caller. "Wrong signature" and "clock too far off" are
// different operational problems and identical security answers.
func verify(md metadata.MD, op Operator, method string, now time.Time) error {
	got := first(md, mdSignature)
	if got == "" {
		return fmt.Errorf("unsigned call")
	}
	ts, err := strconv.ParseInt(first(md, mdTimestamp), 10, 64)
	if err != nil {
		return fmt.Errorf("unreadable timestamp")
	}
	if skew := now.Sub(time.Unix(ts, 0)); skew > signatureWindow || skew < -signatureWindow {
		return fmt.Errorf("timestamp is %s away", skew.Round(time.Second))
	}
	// Constant time: a comparison that returns early on the first wrong byte
	// tells an attacker how much of the signature they have guessed.
	if !hmac.Equal([]byte(got), []byte(sign(op, ts, method))) {
		return fmt.Errorf("signature does not match")
	}
	return nil
}
