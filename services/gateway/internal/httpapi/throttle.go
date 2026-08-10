package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Failure throttling for the routes where an unlimited number of guesses is
// the whole attack.
//
// Login is metered by source address, and that is a change from how this
// started. It was per-account, on the reasoning that a company behind one
// office router shares an IP and an IP budget spent by one person would lock
// out everybody sitting next to them. That reasoning was right about the
// danger and wrong about the conclusion: the danger comes from the window
// being long, not from the key being an address. With a sixty-second window,
// a whole office has to get twenty passwords wrong in one minute to collide,
// and the collision costs them a minute.
//
// So the two counters now split the way ERPNext's do. This one asks "is this
// source attacking" — which is the only question that can catch somebody
// working through a thousand different accounts one guess each, where every
// per-account counter sits at one and nothing ever fires. iam keeps the
// per-account counter, which asks "is this account under attack".
//
// Deliberately not a general request-rate limit. The routes that need one most
// (the tracking pixel, inline images) cannot be metered by identity at all —
// they are meant to be fetched by strangers with no session. Those belong
// upstream, in nginx or a CDN, where per-IP buckets already exist.
const (
	// The name a counter is filed under, so login attempts and mailbox
	// verifications never spend each other's budget.
	throttleLogin      = "login"
	throttleMailVerify = "mailverify"

	// Twenty wrong passwords from one address in a minute. Sized for a shared
	// office egress rather than for one person: twenty people each mistyping
	// once inside the same minute is not a thing that happens, and somebody
	// spraying a list passes it in seconds.
	loginMaxFailures = 20
	loginWindow      = 60 * time.Second

	// Tighter, because each attempt is a real login to Gmail or 263 with the
	// address and code the caller supplied. Guessing here does not just cost
	// us: it spends *our server's IP* against the mail host, and the way that
	// ends is the provider blocking the address every employee sends from.
	// Five is enough for a typo and a retype.
	mailVerifyMaxFailures = 5
	mailVerifyWindow      = 15 * time.Minute
)

// isRejectedCredential separates "you got it wrong" from "we are broken".
//
// Only the first belongs in a failure budget. Every other error — iam
// unreachable, a deadline, a panic downstream — is our fault, and charging it
// to the person at the keyboard means an outage is followed by a company that
// cannot log in for fifteen minutes because of the outage.
func isRejectedCredential(err error) bool {
	switch status.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied:
		return true
	default:
		return false
	}
}

// failureCounter is the throttle's storage. An interface because the decisions
// worth testing are about counts and windows, and standing up a Redis to
// assert "eleven is more than ten" would test Redis.
type failureCounter interface {
	// Bump records one failure and reports the running total and what is left
	// of its window.
	Bump(ctx context.Context, key string, window time.Duration) (int64, time.Duration, error)
	// Peek reports the same without recording anything.
	Peek(ctx context.Context, key string) (int64, time.Duration, error)
	// Clear forgets an identity's failures.
	Clear(ctx context.Context, key string) error
}

// FailureThrottle blocks an identity that has failed too often, and forgets it
// the moment it succeeds.
type FailureThrottle struct {
	c   failureCounter
	log *slog.Logger
}

func NewFailureThrottle(addr string, log *slog.Logger) *FailureThrottle {
	return &FailureThrottle{
		c:   &redisFailureCounter{rdb: redis.NewClient(&redis.Options{Addr: addr})},
		log: log,
	}
}

func throttleLimit(scope string) (int64, time.Duration) {
	if scope == throttleMailVerify {
		return mailVerifyMaxFailures, mailVerifyWindow
	}
	return loginMaxFailures, loginWindow
}

// throttleKey hashes the identity rather than spelling it out.
//
// Two reasons, and the second is the one that matters. A username arrives from
// an unauthenticated request body, so it is whatever the caller typed —
// hashing bounds its length and keeps its bytes out of the keyspace. And a
// Redis full of plaintext usernames is a list of who has an account here,
// sitting in a store that exists for throwaway state.
func throttleKey(scope, id string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(id))))
	return fmt.Sprintf("erp.throttle.%s.%s", scope, hex.EncodeToString(sum[:16]))
}

// Blocked reports whether this identity has already spent its budget, and how
// long is left of the window if so.
//
// A nil throttle, or a counter that will not answer, allows the attempt. This
// is the opposite of how the mailbox unlock store fails, and on purpose: a
// missing unlock store means "we cannot show this person controls the
// mailbox", which has to deny. A missing throttle means "we cannot count",
// and denying on that would turn a Redis hiccup into nobody in the company
// being able to log in.
func (t *FailureThrottle) Blocked(ctx context.Context, scope, id string) (time.Duration, bool) {
	if t == nil || id == "" {
		return 0, false
	}
	max, _ := throttleLimit(scope)
	n, ttl, err := t.c.Peek(ctx, throttleKey(scope, id))
	if err != nil {
		t.warn("could not read the failure count, so allowing the attempt", scope, err)
		return 0, false
	}
	if n < max {
		return 0, false
	}
	return ttl, true
}

// Failed records one failed attempt and reports whether that was the one that
// spent the budget, so the caller can answer "too many" instead of "wrong"
// without a second round trip.
func (t *FailureThrottle) Failed(ctx context.Context, scope, id string) (time.Duration, bool) {
	if t == nil || id == "" {
		return 0, false
	}
	max, window := throttleLimit(scope)
	n, ttl, err := t.c.Bump(ctx, throttleKey(scope, id), window)
	if err != nil {
		t.warn("could not record a failed attempt", scope, err)
		return 0, false
	}
	if n < max {
		return 0, false
	}
	return ttl, true
}

// Passed forgets this identity's failures.
//
// Because the count is of *consecutive* failures. Somebody who mistypes their
// password nine times and then gets it right is not nine tenths of the way to
// being locked out; they are somebody who logs in here.
func (t *FailureThrottle) Passed(ctx context.Context, scope, id string) {
	if t == nil || id == "" {
		return
	}
	if err := t.c.Clear(ctx, throttleKey(scope, id)); err != nil {
		t.warn("could not clear a failure count", scope, err)
	}
}

func (t *FailureThrottle) warn(msg, scope string, err error) {
	if t.log != nil {
		t.log.Warn(msg, "scope", scope, "err", err)
	}
}

// redisFailureCounter is the real storage: one key per identity, expiring on
// its own so nothing has to sweep it.
type redisFailureCounter struct{ rdb *redis.Client }

// Bump increments and, on the first failure of a window, starts the clock.
//
// The window is fixed rather than sliding: it opens at the first failure and
// closes for everybody at once. A sliding window would need the timestamps of
// every attempt, and the extra precision buys nothing here — the question is
// "has this account been hammered lately", not exactly when.
//
// EXPIRE is sent with NX so a later failure inside the window cannot push the
// end of it further out. Without that, an attacker who keeps guessing keeps
// resetting their own lockout to a full fifteen minutes — which is the
// intended punishment, but it also means the *legitimate* owner's account
// stays locked for as long as somebody is attacking it.
func (c *redisFailureCounter) Bump(ctx context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	pipe := c.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.ExpireNX(ctx, key, window)
	ttl := pipe.TTL(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, 0, err
	}
	return incr.Val(), positiveTTL(ttl.Val(), window), nil
}

func (c *redisFailureCounter) Peek(ctx context.Context, key string) (int64, time.Duration, error) {
	pipe := c.rdb.TxPipeline()
	get := pipe.Get(ctx, key)
	ttl := pipe.TTL(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return 0, 0, err
	}
	n, err := get.Int64()
	if err == redis.Nil {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	return n, positiveTTL(ttl.Val(), 0), nil
}

func (c *redisFailureCounter) Clear(ctx context.Context, key string) error {
	return c.rdb.Del(ctx, key).Err()
}

// positiveTTL turns Redis's "no key" (-2) and "no expiry" (-1) into something
// a Retry-After header can carry.
func positiveTTL(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}

// clientAddr is who is asking, as well as this layer can tell.
//
// RemoteAddr by default, because it is the only value the caller cannot
// choose. X-Forwarded-For is trivially forged — anybody can send one — so
// reading it unconditionally would turn the per-source limit into a per-header
// limit, which is no limit at all: a sprayer would put a different fake
// address on every request and never spend a budget.
//
// So it is read only when the deployment says a proxy is in front, and the
// leftmost entry is taken, which is what a proxy that appends its own view
// writes. That is correct exactly when the proxy overwrites rather than
// appends — which nginx's `proxy_set_header X-Forwarded-For $remote_addr`
// does, and which is the configuration this flag is documenting a dependency
// on. Turn it on without that and the limit is spoofable again.
func clientAddr(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			if first, _, found := strings.Cut(fwd, ","); found {
				return strings.TrimSpace(first)
			}
			return strings.TrimSpace(fwd)
		}
	}
	// The port varies per connection and would give every attempt its own
	// bucket, so only the host half is the identity.
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
