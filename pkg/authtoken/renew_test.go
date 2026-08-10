package authtoken

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "a-secret-long-enough-for-a-test"

// The property the whole sliding-session design rests on: renewal must not
// move the moment revocation compares against.
//
// If it did, the sequence is: an administrator revokes; the gateway's snapshot
// is up to ten seconds stale; one request slips through in that window and is
// handed a token stamped "now", which is *after* the revocation. From then on
// the session is permanently immune and renews itself forever. The ten-second
// window would become a permanent bypass.
func TestRenewalCarriesTheSignInMomentForward(t *testing.T) {
	signedIn := time.Now().Add(-3 * time.Hour)

	raw, err := Issue(testSecret, time.Hour, 1, 42, "李娜", "lina@example.com")
	if err != nil {
		t.Fatal(err)
	}
	first, err := Parse(testSecret, raw)
	if err != nil {
		t.Fatal(err)
	}
	// Pretend this token was minted three hours ago.
	first.AuthTime = signedIn.Unix()

	// Renew it several times, as an active session would over a working day.
	claims := first
	for i := range 5 {
		next, err := Renew(testSecret, time.Hour, claims)
		if err != nil {
			t.Fatalf("renewal %d: %v", i, err)
		}
		claims, err = Parse(testSecret, next)
		if err != nil {
			t.Fatalf("renewal %d did not parse: %v", i, err)
		}
		if got := claims.SignedInAt().Unix(); got != signedIn.Unix() {
			t.Fatalf("renewal %d moved the sign-in moment to %v, want %v",
				i, claims.SignedInAt(), signedIn)
		}
	}

	// The two must have come apart: IssuedAt tracks the renewal (now),
	// SignedInAt stays three hours back. Comparing IssuedAt against the
	// previous IssuedAt would prove nothing here — jwt stores seconds, and
	// six mints inside one second are all equal.
	if !claims.IssuedAt.Time.After(claims.SignedInAt().Add(time.Hour)) {
		t.Fatalf("IssuedAt %v did not move away from the sign-in moment %v",
			claims.IssuedAt.Time, claims.SignedInAt())
	}
	// And a revocation recorded one second after sign-in still catches it,
	// five renewals later.
	if !claims.SignedInAt().Before(signedIn.Add(time.Second)) {
		t.Fatal("a revocation one second after sign-in would not catch this token")
	}
}

func TestRenewalKeepsTheIdentityAndRestartsTheClock(t *testing.T) {
	raw, err := Issue(testSecret, 30*time.Minute, 7, 99, "王强", "wq@example.com")
	if err != nil {
		t.Fatal(err)
	}
	old, err := Parse(testSecret, raw)
	if err != nil {
		t.Fatal(err)
	}
	next, err := Renew(testSecret, 2*time.Hour, old)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(testSecret, next)
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != 7 || got.EmployeeID() != 99 ||
		got.EmployeeName != "王强" || got.Email != "wq@example.com" {
		t.Fatalf("identity changed across renewal: %+v", got)
	}
	// The new token must outlive the old one, or renewal achieves nothing.
	if !got.ExpiresAt.Time.After(old.ExpiresAt.Time) {
		t.Fatalf("renewed expiry %v is not later than %v",
			got.ExpiresAt.Time, old.ExpiresAt.Time)
	}
}

// A token minted before auth_time existed still has to work, and its IssuedAt
// is the sign-in moment because nothing renewed back then.
func TestATokenWithoutAuthTimeFallsBackToItsIssueTime(t *testing.T) {
	raw, err := Issue(testSecret, time.Hour, 1, 5, "旧票", "old@example.com")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(testSecret, raw)
	if err != nil {
		t.Fatal(err)
	}
	c.AuthTime = 0 // as an old token would arrive

	if got, want := c.SignedInAt().Unix(), c.IssuedAt.Time.Unix(); got != want {
		t.Fatalf("SignedInAt is %d, want the issue time %d", got, want)
	}
	next, err := Renew(testSecret, time.Hour, c)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := Parse(testSecret, next)
	if err != nil {
		t.Fatal(err)
	}
	// The old token's issue time must have been promoted into auth_time,
	// otherwise the very first renewal launders it into a fresh session.
	if got, want := renewed.SignedInAt().Unix(), c.IssuedAt.Time.Unix(); got != want {
		t.Fatalf("renewing an old token moved its sign-in moment to %d, want %d", got, want)
	}
}

// Refusing is the safe answer: stamping "now" would produce a token no
// revocation could reach.
func TestATokenWithNoTimesAtAllIsNotRenewed(t *testing.T) {
	if _, err := Renew(testSecret, time.Hour, &Claims{TenantID: 1}); err == nil {
		t.Fatal("a token with no issue time was renewed")
	}
}

func TestRenewalStartsAtTheMidpoint(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	at := func(mins int) *Claims {
		return &Claims{RegisteredClaims: registered(now.Add(time.Duration(-mins)*time.Minute), 60)}
	}
	cases := []struct {
		name string
		age  int // minutes since issue, on a 60-minute token
		want bool
	}{
		{"just issued", 0, false},
		{"a third through", 20, false},
		{"one second short of half", 29, false},
		{"just past half", 31, true},
		{"nearly expired", 59, true},
	}
	for _, c := range cases {
		if got := at(c.age).HalfSpent(now); got != c.want {
			t.Errorf("%s: HalfSpent=%v, want %v", c.name, got, c.want)
		}
	}
	// Nothing to measure against: never renew rather than renew always.
	if (&Claims{}).HalfSpent(now) {
		t.Error("a token with no times was treated as half spent")
	}
}

// registered builds a RegisteredClaims issued at `issued` and lasting `mins`
// minutes — enough for HalfSpent, which reads nothing else.
func registered(issued time.Time, mins int) jwt.RegisteredClaims {
	return jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(issued),
		ExpiresAt: jwt.NewNumericDate(issued.Add(time.Duration(mins) * time.Minute)),
	}
}
