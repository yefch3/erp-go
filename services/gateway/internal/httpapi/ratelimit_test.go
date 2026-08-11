package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// These run against the same Redis the gateway uses. The token bucket lives in
// a Lua script, and a Lua script is exactly the kind of thing that passes
// review and fails in Redis — so it is exercised where it will actually run.
//
// Run with: GATEWAY_TEST_REDIS=127.0.0.1:6380
func rateLimiter(t *testing.T) *RateLimiter {
	t.Helper()
	addr := os.Getenv("GATEWAY_TEST_REDIS")
	if addr == "" {
		t.Skip("set GATEWAY_TEST_REDIS to a reachable Redis")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not reachable: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	return &RateLimiter{rdb: rdb, log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
}

// The property that made a bucket the right shape here: pressing twice in a
// row is fine, pressing all afternoon is not. A fixed window would refuse the
// second press of a pair and then hand out a whole fresh allowance the moment
// the window turned over.
func TestSyncAllowsASmallBurstThenPaces(t *testing.T) {
	r := rateLimiter(t)
	ctx := context.Background()
	// A tenant/employee pair nothing else uses, cleared before and after.
	const tenant, employee = int64(990001), int64(990001)
	key := "erp.rl.sync.t990001.e990001"
	_ = r.rdb.Del(ctx, key).Err()
	t.Cleanup(func() { _ = r.rdb.Del(ctx, key).Err() })

	for i := range syncBurst {
		if _, ok := r.AllowSync(ctx, tenant, employee); !ok {
			t.Fatalf("press %d of the burst was refused", i+1)
		}
	}
	wait, ok := r.AllowSync(ctx, tenant, employee)
	if ok {
		t.Fatal("the bucket handed out more than its burst")
	}
	// And it says when to come back, rather than only saying no.
	if wait <= 0 || wait > syncRefillEach {
		t.Fatalf("retry-after is %v, want between 0 and %v", wait, syncRefillEach)
	}
}

// Refill is by elapsed time, not by a window turning over. Simulated by moving
// the bucket's own clock back rather than by sleeping twenty seconds.
func TestSyncRefillsWithTime(t *testing.T) {
	r := rateLimiter(t)
	ctx := context.Background()
	const tenant, employee = int64(990002), int64(990002)
	key := "erp.rl.sync.t990002.e990002"
	_ = r.rdb.Del(ctx, key).Err()
	t.Cleanup(func() { _ = r.rdb.Del(ctx, key).Err() })

	for range syncBurst {
		r.AllowSync(ctx, tenant, employee)
	}
	if _, ok := r.AllowSync(ctx, tenant, employee); ok {
		t.Fatal("expected the bucket to be empty")
	}

	// Two refill periods ago: two tokens should have accrued, and no more.
	past := time.Now().Add(-2 * syncRefillEach).Unix()
	if err := r.rdb.HSet(ctx, key, "seen", past).Err(); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if _, ok := r.AllowSync(ctx, tenant, employee); !ok {
			t.Fatalf("token %d did not refill", i+1)
		}
	}
	if _, ok := r.AllowSync(ctx, tenant, employee); ok {
		t.Fatal("more tokens accrued than time allows")
	}
}

// The bucket must never exceed its burst however long it sits idle, or a
// mailbox left alone overnight comes back with an unbounded allowance.
func TestTheBucketDoesNotOverfill(t *testing.T) {
	r := rateLimiter(t)
	ctx := context.Background()
	const tenant, employee = int64(990003), int64(990003)
	key := "erp.rl.sync.t990003.e990003"
	_ = r.rdb.Del(ctx, key).Err()
	t.Cleanup(func() { _ = r.rdb.Del(ctx, key).Err() })

	r.AllowSync(ctx, tenant, employee)
	// A week of idleness.
	if err := r.rdb.HSet(ctx, key, "seen", time.Now().Add(-7*24*time.Hour).Unix()).Err(); err != nil {
		t.Fatal(err)
	}
	allowed := 0
	for range syncBurst + 5 {
		if _, ok := r.AllowSync(ctx, tenant, employee); ok {
			allowed++
		}
	}
	if allowed != syncBurst {
		t.Fatalf("a week idle bought %d presses, want exactly the burst of %d", allowed, syncBurst)
	}
}

// One person's pacing must not touch anybody else's, the same property the
// session revocation has and for the same reason.
func TestPacingIsPerPerson(t *testing.T) {
	r := rateLimiter(t)
	ctx := context.Background()
	keys := []string{"erp.rl.sync.t990004.e1", "erp.rl.sync.t990004.e2"}
	for _, k := range keys {
		_ = r.rdb.Del(ctx, k).Err()
	}
	t.Cleanup(func() {
		for _, k := range keys {
			_ = r.rdb.Del(ctx, k).Err()
		}
	})
	for range syncBurst + 2 {
		r.AllowSync(ctx, 990004, 1)
	}
	if _, ok := r.AllowSync(ctx, 990004, 2); !ok {
		t.Fatal("one colleague's pacing blocked another's")
	}
}

// A nil limiter allows everything. Being unable to count is not a reason to
// stop people working — the opposite of how the unlock store fails.
func TestANilLimiterAllowsEverything(t *testing.T) {
	var r *RateLimiter
	if _, ok := r.AllowSync(context.Background(), 1, 1); !ok {
		t.Error("a nil limiter refused a sync")
	}
	if _, ok := r.AllowPublic(context.Background(), "img", "1.2.3.4", 10); !ok {
		t.Error("a nil limiter refused a public request")
	}
}

// The public budget is a plain counter over a minute, and it must refuse only
// after the budget is spent — not on the request that reaches it.
func TestThePublicBudgetRefusesOnlyOnceSpent(t *testing.T) {
	r := rateLimiter(t)
	ctx := context.Background()
	const addr = "203.0.113.99"
	// Same key derivation as the code under test, so cleanup finds it.
	t.Cleanup(func() {
		iter := r.rdb.Scan(ctx, 0, "erp.rl.imgtest.*", 100).Iterator()
		for iter.Next(ctx) {
			_ = r.rdb.Del(ctx, iter.Val()).Err()
		}
	})
	for i := range 3 {
		if _, ok := r.AllowPublic(ctx, "imgtest", addr, 3); !ok {
			t.Fatalf("request %d of the budget was refused", i+1)
		}
	}
	wait, ok := r.AllowPublic(ctx, "imgtest", addr, 3)
	if ok {
		t.Fatal("the budget let through more than it should")
	}
	if wait <= 0 || wait > publicWindow {
		t.Fatalf("retry-after is %v, want within the window", wait)
	}
}

// Different sources have different budgets — otherwise one busy mail provider
// would spend everybody's.
func TestPublicBudgetsArePerSource(t *testing.T) {
	r := rateLimiter(t)
	ctx := context.Background()
	t.Cleanup(func() {
		iter := r.rdb.Scan(ctx, 0, "erp.rl.imgtest2.*", 100).Iterator()
		for iter.Next(ctx) {
			_ = r.rdb.Del(ctx, iter.Val()).Err()
		}
	})
	for range 3 {
		r.AllowPublic(ctx, "imgtest2", "198.51.100.1", 3)
	}
	if _, ok := r.AllowPublic(ctx, "imgtest2", "198.51.100.2", 3); !ok {
		t.Fatal("one source's budget blocked another's")
	}
}

// The wrapper has to answer 429 with a Retry-After, because the caller is
// usually a mail client or a provider's proxy and both back off correctly when
// told how long. Answering nothing invites an immediate retry.
func TestARefusedPublicRequestSaysWhenToComeBack(t *testing.T) {
	r := rateLimiter(t)
	ctx := context.Background()
	t.Cleanup(func() {
		iter := r.rdb.Scan(ctx, 0, "erp.rl.wrap.*", 100).Iterator()
		for iter.Next(ctx) {
			_ = r.rdb.Del(ctx, iter.Val()).Err()
		}
	})
	s := &Server{Limits: r, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	served := 0
	h := s.limitPublic("wrap", 1, func(w http.ResponseWriter, _ *http.Request) {
		served++
		w.WriteHeader(http.StatusOK)
	})

	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/public/mail-images/x", nil)
		req.RemoteAddr = "192.0.2.55:1234"
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}
	if rec := call(); rec.Code != http.StatusOK {
		t.Fatalf("first request got %d", rec.Code)
	}
	rec := call()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request got %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After, so a client has nothing to wait on")
	}
	if served != 1 {
		t.Errorf("the handler ran %d times, want 1 — a refused request must not reach it", served)
	}
}
