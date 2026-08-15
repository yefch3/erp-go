package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// What the cookie session must hold to be worth having:
//
//  1. a session cookie authenticates exactly like the header did
//  2. a cookie-borne MUTATING request without its CSRF echo is refused —
//     that refusal IS the CSRF defence; a version of this file where that
//     test passes vacuously is a version where any website can write to the
//     ERP with the reader's own session
//  3. a header-authenticated caller owes no CSRF proof (no foreign page can
//     set a header)
//  4. renewal of a cookie session travels as Set-Cookie, never as the old
//     response header a hooked XMLHttpRequest could read
//  5. login's Set-Cookie pair carries the right flags: the session is
//     script-invisible, the CSRF value deliberately is not

func cookieRequest(method, token, csrfCookie, csrfHeader string) *http.Request {
	req := httptest.NewRequest(method, "/api/whatever", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: token})
	if csrfCookie != "" {
		req.AddCookie(&http.Cookie{Name: CSRFCookie, Value: csrfCookie})
	}
	if csrfHeader != "" {
		req.Header.Set(CSRFHeader, csrfHeader)
	}
	return req
}

func serve(s *Server, req *http.Request) *httptest.ResponseRecorder {
	h := s.auth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCookieAuthenticatesReads(t *testing.T) {
	s := renewServer(t, time.Hour, nil)
	tok := tokenAged(t, time.Minute, 12*time.Hour, 42, time.Time{})
	rec := serve(s, cookieRequest(http.MethodGet, tok, "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("a cookie session could not read: status %d", rec.Code)
	}
}

func TestCookieMutationsDemandTheCSRFEcho(t *testing.T) {
	s := renewServer(t, time.Hour, nil)
	tok := tokenAged(t, time.Minute, 12*time.Hour, 42, time.Time{})

	// No echo at all: this is what a foreign page's form post looks like.
	rec := serve(s, cookieRequest(http.MethodPost, tok, "abc", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a POST without the CSRF echo passed: status %d", rec.Code)
	}
	// A wrong echo: a foreign page guessing. It cannot read the cookie, so
	// guessing is all it has.
	rec = serve(s, cookieRequest(http.MethodPost, tok, "abc", "wrong"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a POST with a wrong CSRF echo passed: status %d", rec.Code)
	}
	// The echo our own page produces.
	rec = serve(s, cookieRequest(http.MethodPost, tok, "abc", "abc"))
	if rec.Code != http.StatusOK {
		t.Fatalf("a POST with the correct echo was refused: status %d", rec.Code)
	}
	// DELETE is as mutating as POST; the method list must not be POST-only.
	rec = serve(s, cookieRequest(http.MethodDelete, tok, "abc", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a DELETE without the echo passed: status %d", rec.Code)
	}
}

func TestHeaderCallersOweNoCSRF(t *testing.T) {
	s := renewServer(t, time.Hour, nil)
	tok := tokenAged(t, time.Minute, 12*time.Hour, 42, time.Time{})
	req := httptest.NewRequest(http.MethodPost, "/api/whatever", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := serve(s, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("a header-authenticated POST was refused: status %d", rec.Code)
	}
}

// A dead session answers 401 "sign in again" even when the CSRF echo is also
// missing: telling somebody with an expired token to refresh the page sends
// them in a circle.
func TestADeadCookieSessionSaysSignInNotRefresh(t *testing.T) {
	s := renewServer(t, time.Hour, nil)
	dead := tokenAged(t, 2*time.Hour, time.Hour, 42, time.Time{})
	rec := serve(s, cookieRequest(http.MethodPost, dead, "abc", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401 before any CSRF answer", rec.Code)
	}
}

func TestCookieRenewalTravelsAsSetCookieOnly(t *testing.T) {
	s := renewServer(t, time.Hour, nil)
	old := tokenAged(t, 10*time.Hour, 12*time.Hour, 42, time.Time{})
	req := cookieRequest(http.MethodGet, old, "keepme", "")
	rec := serve(s, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get(RenewedTokenHeader); got != "" {
		t.Fatalf("renewal leaked into the readable header: %q", got)
	}
	var session, csrf *http.Cookie
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case SessionCookie:
			session = c
		case CSRFCookie:
			csrf = c
		}
	}
	if session == nil || session.Value == "" {
		t.Fatal("a half-spent cookie session was not renewed as a cookie")
	}
	if !session.HttpOnly {
		t.Fatal("the renewed session cookie is script-readable")
	}
	// The CSRF value survives renewal unchanged — the page captured it once;
	// rotating it here would fail whatever requests are already in flight —
	// but its clock must be wound back up alongside the session's.
	if csrf == nil || csrf.Value != "keepme" {
		t.Fatalf("the CSRF cookie was not re-extended with its own value: %+v", csrf)
	}

	// A header session keeps renewing over the header, untouched by any of
	// this: scripts and tooling did not sign up for cookies.
	req = httptest.NewRequest(http.MethodGet, "/api/whatever", nil)
	req.Header.Set("Authorization", "Bearer "+old)
	rec = serve(s, req)
	if rec.Header().Get(RenewedTokenHeader) == "" {
		t.Fatal("a header session stopped renewing")
	}
}

func TestLoginCookiePairCarriesTheRightFlags(t *testing.T) {
	s := renewServer(t, time.Hour, nil)
	rec := httptest.NewRecorder()
	s.setSessionCookies(rec, "the-token")
	var session, csrf *http.Cookie
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case SessionCookie:
			session = c
		case CSRFCookie:
			csrf = c
		}
	}
	if session == nil || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie flags wrong: %+v", session)
	}
	if csrf == nil || csrf.HttpOnly || csrf.SameSite != http.SameSiteLaxMode || len(csrf.Value) != 32 {
		t.Fatalf("csrf cookie flags wrong: %+v", csrf)
	}

	// Logout must actually expire both — an httpOnly cookie is the one thing
	// the page itself cannot delete.
	rec = httptest.NewRecorder()
	s.clearSessionCookies(rec)
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge >= 0 || c.Value != "" {
			t.Fatalf("logout left %s alive: %+v", c.Name, c)
		}
	}
}

// The full middleware order on one request: a cross-site-shaped POST against
// a revoked session must be told "signed out", and must not be renewed on
// its way through any branch.
func TestRevocationOutranksCSRFAndRenewal(t *testing.T) {
	s := renewServer(t, time.Hour, offlineStore(map[string]int64{
		snapshotKey(1, 42): time.Now().Add(time.Minute).Unix(),
	}))
	old := tokenAged(t, 10*time.Hour, 12*time.Hour, 42, time.Time{})
	rec := serve(s, cookieRequest(http.MethodPost, old, "abc", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "CSRF") {
		t.Fatal("a revoked session was answered about CSRF instead of about itself")
	}
	if len(rec.Result().Cookies()) != 0 || rec.Header().Get(RenewedTokenHeader) != "" {
		t.Fatal("a revoked session was renewed on its way out")
	}
}
