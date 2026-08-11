package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Two limits with opposite shapes, because they are protecting opposite things.
//
// **"立即收信" is rare and expensive.** Each press is a real IMAP round trip
// against somebody else's server. The fleet's deduplication already stops five
// simultaneous presses from becoming five syncs, but nothing stopped one press
// a second — and a stuck auto-refresh script needs no malice to saturate a
// mailbox's own connection. A token bucket fits: pressing twice in a row is a
// reasonable thing to do, pressing all afternoon is not, and a bucket says
// exactly that. A fixed window would refuse the second press of a pair and
// then hand out a fresh allowance the instant the window turned over.
//
// **The public routes are frequent and cheap.** They exist to be fetched by
// strangers — a recipient's mail client has no session. Here the thing worth
// protecting is bandwidth, the per-request budget is tiny, and a plain counter
// with an expiry is the right cost. Its known flaw, letting through up to
// twice the limit across a window boundary, does not matter for a bandwidth
// guard.
//
// ---
//
// A trap worth spelling out, because tightening these numbers later without
// knowing it would break something that currently works:
//
//	**Gmail does not let recipients fetch our images directly.** Its own
//	servers fetch and cache on their behalf. So a thousand Gmail recipients
//	arrive as a handful of googleusercontent addresses. Set the per-address
//	limit tight and what gets cut off is every Gmail recipient's pictures and
//	open tracking — not an attacker.
//
// The ceilings below are therefore deliberately generous: they are here to
// stop a scanner hammering us, not to shape normal traffic. Since inline
// images now travel with the message (see the mail service's InlineMailImages),
// only older mail uses the image route at all, so the exposure is smaller than
// these numbers suggest.
const (
	// One press every twenty seconds sustained, three saved up.
	syncBurst      = 3
	syncRefillEach = 20 * time.Second

	publicWindow      = time.Minute
	publicImageBudget = 600
	publicPixelBudget = 600
)

// RateLimiter is a token bucket and a fixed-window counter over Redis.
//
// Nil is allowed and permits everything, matching FailureThrottle rather than
// UnlockStore: being unable to count is not a reason to stop people working.
// A rate limit exists to bound a cost, and a Redis hiccup that stopped the
// company reading its mail would cost far more than the thing it is guarding.
type RateLimiter struct {
	rdb *redis.Client
	log *slog.Logger
}

func NewRateLimiter(addr string, log *slog.Logger) *RateLimiter {
	return &RateLimiter{rdb: redis.NewClient(&redis.Options{Addr: addr}), log: log}
}

// takeToken refills by elapsed time, then spends one if there is one.
//
// Lua because refill-and-spend has to be one step. Read-modify-write from Go
// lets two presses landing together both see the same token and both take it,
// which is precisely the burst the limit exists to bound.
//
// Returns the seconds until the next token when it refuses, so the caller can
// say when to come back instead of just "no".
var takeToken = redis.NewScript(`
local key      = KEYS[1]
local burst    = tonumber(ARGV[1])
local refill   = tonumber(ARGV[2])  -- seconds per token
local now      = tonumber(ARGV[3])

local tokens = tonumber(redis.call('HGET', key, 'tokens'))
local seen   = tonumber(redis.call('HGET', key, 'seen'))
if tokens == nil or seen == nil then
  tokens = burst
  seen = now
end

-- Whole tokens only: carrying a fraction would need the remainder stored too,
-- and at this granularity it buys nothing.
local gained = math.floor((now - seen) / refill)
if gained > 0 then
  tokens = math.min(burst, tokens + gained)
  seen = seen + gained * refill
end

local allowed = 0
local wait = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  wait = math.ceil(seen + refill - now)
  if wait < 1 then wait = 1 end
end

redis.call('HSET', key, 'tokens', tokens, 'seen', seen)
-- Long enough that a full bucket is reachable again; the key is disposable.
redis.call('EXPIRE', key, math.ceil(burst * refill) + refill)
return {allowed, wait}
`)

// AllowSync reports whether this person may trigger a mailbox sync now.
func (r *RateLimiter) AllowSync(ctx context.Context, tenantID, employeeID int64) (time.Duration, bool) {
	if r == nil {
		return 0, true
	}
	key := fmt.Sprintf("erp.rl.sync.t%d.e%d", tenantID, employeeID)
	res, err := takeToken.Run(ctx, r.rdb, []string{key},
		syncBurst, int(syncRefillEach.Seconds()), time.Now().Unix()).Slice()
	if err != nil || len(res) != 2 {
		r.warn("could not check the sync rate limit, allowing it", err)
		return 0, true
	}
	allowed, _ := res[0].(int64)
	wait, _ := res[1].(int64)
	if allowed == 1 {
		return 0, true
	}
	return time.Duration(wait) * time.Second, false
}

// AllowPublic counts one hit against a source's per-minute budget.
//
// Keyed by a hash of the address rather than the address itself: this is an
// unauthenticated route, so the value is whatever reached us, and a Redis full
// of readable IP addresses is a log of who opened which customer's mail — kept
// in the one store that exists for state nobody minds losing. The same
// reasoning as throttleKey.
func (r *RateLimiter) AllowPublic(ctx context.Context, scope, addr string, budget int64) (time.Duration, bool) {
	if r == nil || addr == "" {
		return 0, true
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(addr)))
	key := fmt.Sprintf("erp.rl.%s.%s", scope, hex.EncodeToString(sum[:16]))

	pipe := r.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	// NX so the window is anchored at the first hit of the minute rather than
	// being pushed forward by every hit — otherwise a steady stream never
	// expires the key and one early burst blocks for ever.
	pipe.ExpireNX(ctx, key, publicWindow)
	ttl := pipe.TTL(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil {
		r.warn("could not count a public request, allowing it", err)
		return 0, true
	}
	if incr.Val() <= budget {
		return 0, true
	}
	return positiveTTL(ttl.Val(), publicWindow), false
}

func (r *RateLimiter) warn(msg string, err error) {
	if r != nil && r.log != nil {
		r.log.Warn(msg, "err", err)
	}
}

// limitPublic wraps a public handler in a per-source budget.
//
// Refusing with 429 and a Retry-After rather than silence: the fetcher is
// usually a mail client or a proxy, and both back off correctly when told how
// long. Answering nothing invites an immediate retry.
func (s *Server) limitPublic(scope string, budget int64, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		addr := clientAddr(r, s.TrustProxyHeaders)
		if wait, ok := s.Limits.AllowPublic(r.Context(), scope, addr, budget); !ok {
			// Logged, because the first time this fires in production is the
			// moment to find out whether it caught a scanner or a mail
			// provider's proxy — and the two need opposite responses.
			s.Log.Warn("a public route refused a source for exceeding its budget",
				"scope", scope, "retry_after_seconds", int(wait.Seconds()))
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}
