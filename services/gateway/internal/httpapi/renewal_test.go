package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/sgao19/erp-go/pkg/authtoken"
)

const renewSecret = "a-secret-long-enough-for-a-gateway-test"

func renewServer(t *testing.T, ttl time.Duration, rev *RevocationStore) *Server {
	t.Helper()
	return &Server{
		JWTSecret:   renewSecret,
		TokenTTL:    ttl,
		Revocations: rev,
		Log:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
}

// tokenAged mints a real, correctly signed token with times chosen by the
// test rather than by the clock.
//
// authtoken.Issue always stamps IssuedAt as now, and jwt stores both times to
// the nearest second — so "issue with a one-second life and hope it is past
// halfway" is a coin flip on where inside the current second the test lands.
// It passed, then failed, on identical code. Choosing the times outright makes
// the assertion about the rule instead of about the scheduler.
func tokenAged(t *testing.T, issuedAgo, life time.Duration, employeeID int64, authTime time.Time) string {
	t.Helper()
	issued := time.Now().Add(-issuedAgo)
	if authTime.IsZero() {
		authTime = issued
	}
	claims := authtoken.Claims{
		TenantID: 1,
		AuthTime: authTime.Unix(),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(employeeID, 10),
			IssuedAt:  jwt.NewNumericDate(issued),
			ExpiresAt: jwt.NewNumericDate(issued.Add(life)),
			Issuer:    "erp-iam",
		},
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(renewSecret))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// authed runs one request through the real auth middleware and reports the
// status and whatever renewal header came back.
func authed(t *testing.T, s *Server, token string) (int, string) {
	t.Helper()
	h := s.auth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/whatever", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Header().Get(RenewedTokenHeader)
}

// A fresh token is not renewed, a half-spent one is. Renewing on every request
// would sign a token that is discarded seconds later; renewing only at the
// last moment loses the session whenever the final request happens to fail.
func TestATokenIsRenewedOnlyOncePastHalfway(t *testing.T) {
	s := renewServer(t, time.Hour, nil)

	// Ten minutes into a twelve-hour life: nowhere near halfway.
	fresh := tokenAged(t, 10*time.Minute, 12*time.Hour, 42, time.Time{})
	if code, hdr := authed(t, s, fresh); code != http.StatusOK || hdr != "" {
		t.Fatalf("a fresh token got status %d and header %q, want 200 and none", code, hdr)
	}

	// Ten hours into a twelve-hour life: well past it, still valid.
	old := tokenAged(t, 10*time.Hour, 12*time.Hour, 42, time.Time{})
	code, hdr := authed(t, s, old)
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if hdr == "" {
		t.Fatal("a half-spent token was not renewed")
	}
	renewed, err := authtoken.Parse(renewSecret, hdr)
	if err != nil {
		t.Fatalf("the renewed token does not parse: %v", err)
	}
	if renewed.EmployeeID() != 42 || renewed.TenantID != 1 {
		t.Fatalf("the renewed token is for somebody else: %+v", renewed)
	}
	// It must be good for the server's configured life, not for whatever
	// remained of the old one.
	if left := time.Until(renewed.ExpiresAt.Time); left < 50*time.Minute {
		t.Fatalf("the renewed token only has %v left", left)
	}
}

// The trap this whole design exists to avoid.
//
// The revocation snapshot is up to ten seconds stale. If a request slips
// through in that window and is handed a token stamped "now", and revocation
// compared against IssuedAt, that token would sit *after* the cutoff and the
// session would be immune from then on — renewing itself forever. The
// ten-second window would become a permanent bypass.
func TestARenewedTokenIsStillCaughtByARevocationItSlippedPast(t *testing.T) {
	signedIn := time.Now().Add(-30 * time.Minute)
	// The administrator pulled the plug a minute ago; the snapshot has it.
	revokedAt := time.Now().Add(-time.Minute)

	// A token whose owner is already revoked, but which renewal would stamp
	// with a fresh IssuedAt.
	raw, err := authtoken.Issue(renewSecret, time.Second, 1, 42, "李娜", "l@example.com")
	if err != nil {
		t.Fatal(err)
	}
	c, err := authtoken.Parse(renewSecret, raw)
	if err != nil {
		t.Fatal(err)
	}
	c.AuthTime = signedIn.Unix()

	// Simulate the stale window: renewal happens while the snapshot is empty.
	handedOut, err := authtoken.Renew(renewSecret, time.Hour, c)
	if err != nil {
		t.Fatal(err)
	}

	// Now the snapshot catches up.
	s := renewServer(t, time.Hour, offlineStore(map[string]int64{
		snapshotKey(1, 42): revokedAt.Unix(),
	}))

	code, hdr := authed(t, s, handedOut)
	if code != http.StatusUnauthorized {
		t.Fatalf("a renewed token outran the revocation: status %d", code)
	}
	if hdr != "" {
		t.Fatal("a revoked session was handed another renewal on its way out")
	}
}

// Renewal must come after the revocation check, not before: a session about to
// be refused must not be handed a fresh token on the same response.
func TestARevokedSessionIsNotRenewed(t *testing.T) {
	s := renewServer(t, time.Hour, offlineStore(map[string]int64{
		snapshotKey(1, 42): time.Now().Add(time.Minute).Unix(),
	}))
	// Well past halfway, so renewal would fire if the order were wrong.
	tok := tokenAged(t, 10*time.Hour, 12*time.Hour, 42, time.Time{})
	code, hdr := authed(t, s, tok)
	if code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", code)
	}
	if hdr != "" {
		t.Fatalf("a revoked session was renewed: %q", hdr)
	}
}

// An expired token is rejected by the signature check before any of this, so
// renewal can never resurrect a session that ran out while nobody was looking.
// This is what makes JWT_TTL an idle timeout rather than a formality.
func TestAnExpiredTokenIsNotRenewed(t *testing.T) {
	s := renewServer(t, time.Hour, nil)
	dead, err := authtoken.Issue(renewSecret, -time.Minute, 1, 42, "李娜", "l@example.com")
	if err != nil {
		t.Fatal(err)
	}
	code, hdr := authed(t, s, dead)
	if code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", code)
	}
	if hdr != "" {
		t.Fatal("an expired session was renewed back to life")
	}
}
