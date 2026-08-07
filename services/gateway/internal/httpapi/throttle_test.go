package httpapi

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeCounter is the storage the throttle would otherwise get from Redis. The
// decisions worth testing here are about counts and windows; standing up a
// Redis to assert that eleven is more than ten would be testing Redis.
type fakeCounter struct {
	n      map[string]int64
	ttl    map[string]time.Duration
	broken error
	// Every key Bump has been called with, so a test can prove which identity
	// a budget was spent against rather than only that one was.
	bumped []string
}

func newFakeCounter() *fakeCounter {
	return &fakeCounter{n: map[string]int64{}, ttl: map[string]time.Duration{}}
}

func (f *fakeCounter) Bump(_ context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	if f.broken != nil {
		return 0, 0, f.broken
	}
	f.bumped = append(f.bumped, key)
	f.n[key]++
	if _, open := f.ttl[key]; !open {
		f.ttl[key] = window
	}
	return f.n[key], f.ttl[key], nil
}

func (f *fakeCounter) Peek(_ context.Context, key string) (int64, time.Duration, error) {
	if f.broken != nil {
		return 0, 0, f.broken
	}
	return f.n[key], f.ttl[key], nil
}

func (f *fakeCounter) Clear(_ context.Context, key string) error {
	if f.broken != nil {
		return f.broken
	}
	delete(f.n, key)
	delete(f.ttl, key)
	return nil
}

func newTestThrottle() (*FailureThrottle, *fakeCounter) {
	c := newFakeCounter()
	return &FailureThrottle{c: c}, c
}

func TestTheBudgetIsSpentOnlyOnTheLastFailure(t *testing.T) {
	th, _ := newTestThrottle()
	ctx := context.Background()

	for i := 1; i < loginMaxFailures; i++ {
		if _, spent := th.Failed(ctx, throttleLogin, "kratos"); spent {
			t.Fatalf("locked out after %d failures, budget is %d", i, loginMaxFailures)
		}
		if _, blocked := th.Blocked(ctx, throttleLogin, "kratos"); blocked {
			t.Fatalf("blocked after %d failures, budget is %d", i, loginMaxFailures)
		}
	}
	wait, spent := th.Failed(ctx, throttleLogin, "kratos")
	if !spent {
		t.Fatalf("failure %d did not spend the budget", loginMaxFailures)
	}
	if wait != loginWindow {
		t.Fatalf("retry-after = %v, want the window %v", wait, loginWindow)
	}
	if _, blocked := th.Blocked(ctx, throttleLogin, "kratos"); !blocked {
		t.Fatal("a later attempt was allowed through after the budget was spent")
	}
}

func TestSucceedingForgetsEarlierFailures(t *testing.T) {
	th, _ := newTestThrottle()
	ctx := context.Background()

	for i := 0; i < loginMaxFailures-1; i++ {
		th.Failed(ctx, throttleLogin, "kratos")
	}
	th.Passed(ctx, throttleLogin, "kratos")

	// The count is of consecutive failures: somebody who mistypes nine times
	// and then logs in is not one mistake from a lockout.
	for i := 1; i < loginMaxFailures; i++ {
		if _, spent := th.Failed(ctx, throttleLogin, "kratos"); spent {
			t.Fatalf("locked out after %d failures following a success", i)
		}
	}
}

func TestOneAccountsFailuresDoNotLockAnother(t *testing.T) {
	th, _ := newTestThrottle()
	ctx := context.Background()

	for i := 0; i < loginMaxFailures*2; i++ {
		th.Failed(ctx, throttleLogin, "kratos")
	}
	if _, blocked := th.Blocked(ctx, throttleLogin, "someone-else"); blocked {
		t.Fatal("a colleague was locked out by somebody else's failures")
	}
}

// The two routes have different budgets because they cost different things,
// which is worth nothing if they share a counter.
func TestLoginAndMailboxVerifyDoNotSpendEachOthersBudget(t *testing.T) {
	th, _ := newTestThrottle()
	ctx := context.Background()

	for i := 0; i < mailVerifyMaxFailures; i++ {
		th.Failed(ctx, throttleMailVerify, "t1.e1")
	}
	if _, blocked := th.Blocked(ctx, throttleMailVerify, "t1.e1"); !blocked {
		t.Fatal("mailbox verification was not blocked at its own limit")
	}
	if _, blocked := th.Blocked(ctx, throttleLogin, "t1.e1"); blocked {
		t.Fatal("mailbox failures spent the login budget for the same name")
	}
}

func TestMailboxVerifyIsStricterThanLogin(t *testing.T) {
	// Not a style preference: a verification attempt is a real login to Gmail
	// or 263 from our IP, so it has to cost more than a guess at our own
	// password does.
	if mailVerifyMaxFailures >= loginMaxFailures {
		t.Fatalf("mailbox verify allows %d failures, login allows %d — the one that "+
			"spends our server's reputation with the mail host must be tighter",
			mailVerifyMaxFailures, loginMaxFailures)
	}
}

// Sabotage: if the counter's storage goes down, the throttle must not lock the
// whole company out of the ERP. It fails open, unlike the mailbox unlock store,
// and this test is what keeps somebody from "fixing" that inconsistency.
func TestAStorageOutageAllowsAttemptsRatherThanBlockingEveryone(t *testing.T) {
	th, c := newTestThrottle()
	ctx := context.Background()
	c.broken = errors.New("redis is down")

	if _, blocked := th.Blocked(ctx, throttleLogin, "kratos"); blocked {
		t.Fatal("blocked a login because the counter was unreachable")
	}
	if _, spent := th.Failed(ctx, throttleLogin, "kratos"); spent {
		t.Fatal("reported a spent budget from a failed write")
	}
	th.Passed(ctx, throttleLogin, "kratos") // must not panic
}

// Sabotage: a throttle that was never wired up must not silently pretend to
// meter anything, and must not panic on the way past.
func TestAnAbsentThrottleAllowsEverything(t *testing.T) {
	var th *FailureThrottle
	ctx := context.Background()

	if _, blocked := th.Blocked(ctx, throttleLogin, "kratos"); blocked {
		t.Fatal("a nil throttle blocked an attempt")
	}
	if _, spent := th.Failed(ctx, throttleLogin, "kratos"); spent {
		t.Fatal("a nil throttle reported a spent budget")
	}
	th.Passed(ctx, throttleLogin, "kratos")
}

// An empty username is every caller who posted no username at all. Metering
// them together would let one malformed client lock out the next.
func TestAnEmptyIdentityIsNotMetered(t *testing.T) {
	th, c := newTestThrottle()
	ctx := context.Background()

	for i := 0; i < loginMaxFailures*3; i++ {
		th.Failed(ctx, throttleLogin, "")
	}
	if _, blocked := th.Blocked(ctx, throttleLogin, ""); blocked {
		t.Fatal("callers with no username were metered as one identity")
	}
	if len(c.bumped) != 0 {
		t.Fatalf("wrote %d counters for an empty identity", len(c.bumped))
	}
}

func TestTheSameAccountIsOneIdentityHoweverItIsTyped(t *testing.T) {
	// Otherwise the budget is per-spelling, and " Kratos" is a fresh ten
	// guesses.
	for _, id := range []string{"Kratos", " kratos", "KRATOS ", "kratos"} {
		if got, want := throttleKey(throttleLogin, id), throttleKey(throttleLogin, "kratos"); got != want {
			t.Fatalf("%q keyed as %s, want %s", id, got, want)
		}
	}
}

func TestTheStoredKeyDoesNotSpellOutTheAccount(t *testing.T) {
	// The key is what lands in a store meant for throwaway state; a Redis full
	// of plaintext usernames is a list of who has an account here.
	if key := throttleKey(throttleLogin, "kratos"); contains(key, "kratos") {
		t.Fatalf("key %q carries the username", key)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// An outage must not be charged to the people it happens to. Without this, iam
// going down for five minutes ends with everybody in the company locked out
// for fifteen more — a recovery worse than the fault.
func TestOnlyARejectedCredentialCountsAgainstTheBudget(t *testing.T) {
	rejected := []codes.Code{codes.Unauthenticated, codes.PermissionDenied}
	ours := []codes.Code{
		codes.Unavailable, codes.DeadlineExceeded, codes.Internal,
		codes.ResourceExhausted, codes.Unknown, codes.Canceled,
	}
	for _, c := range rejected {
		if !isRejectedCredential(status.Error(c, "")) {
			t.Errorf("%v should count: it is the caller getting it wrong", c)
		}
	}
	for _, c := range ours {
		if isRejectedCredential(status.Error(c, "")) {
			t.Errorf("%v should not count: it is us being broken", c)
		}
	}
	if isRejectedCredential(nil) {
		t.Error("a successful call counted as a failure")
	}
}

// The mailbox somebody binds is the one they signed in as. This is a test
// about a *field*, because that is where the rule lives: the verify handler
// reads the address from the token, and the request body has no address in it
// to disagree with. Anybody adding one back should have to delete this.
func TestTheVerifyRequestCarriesNoAddress(t *testing.T) {
	src, err := os.ReadFile("mailunlock.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "func (s *Server) verifyMailbox")
	if start < 0 {
		t.Fatal("verifyMailbox is gone; this test needs rewriting")
	}
	end := strings.Index(body[start:], "\nfunc ")
	fn := body[start : start+end]

	if strings.Contains(fn, `json:"email"`) {
		t.Error("the verify body has an email field again — signing in as one " +
			"address and binding another is exactly what this must not allow")
	}
	if !strings.Contains(fn, "Email: op.Email") {
		t.Error("the bound address no longer comes from the session; it must " +
			"be the address the person logged in with, not one they supplied")
	}
	if !strings.Contains(fn, `op.Email == ""`) {
		t.Error("an employee with no company address must be refused rather " +
			"than binding something unchecked")
	}
}
