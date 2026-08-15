package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
)

// The session travels in an httpOnly cookie instead of localStorage.
//
// What that buys, precisely: no script running in this origin can read the
// token. Before this, one injected script — and a mail client's attack
// surface is every HTML mail it renders — could lift the token from
// localStorage and replay it from the attacker's own machine until expiry.
// httpOnly closes the theft; the MailBody sandbox and SanitizeHTML remain
// the first line, because a script that got in can still ride the session
// while the page is open. Layers, each covering a different failure.
//
// A cookie the browser attaches automatically re-opens the classic hole the
// localStorage design never had: any site can make this browser send a
// request here, cookie included (CSRF). Two independent locks close it:
//
//   - SameSite=Lax: the browser refuses to attach the cookie to cross-site
//     mutating requests. Lax rather than Strict because mail links must keep
//     working — an activation link opened from a mail client is a cross-site
//     top-level GET, and Strict would greet it signed-out.
//   - Double-submit: a second, deliberately script-READABLE cookie whose
//     value every mutating request must echo in a header. Our page can read
//     it; a foreign origin cannot read cookies for this origin whatever
//     their flags. That asymmetry is the whole trick.
//
// The CSRF cookie holds a random value with no meaning — not derived from
// the token, never checked server-side against anything but its own echo.

const (
	// SessionCookie carries the JWT, invisible to every script in the page.
	SessionCookie = "erp_session"
	// CSRFCookie is readable by our page on purpose — it must be, to be
	// echoed. Foreign origins cannot read it; that is the entire defence.
	CSRFCookie = "erp_csrf"
	// CSRFHeader is where the page echoes the cookie on mutating requests.
	CSRFHeader = "X-CSRF-Token"
)

// mutating says whether a method changes state. GET/HEAD/OPTIONS ride along
// on cross-site navigations by design; everything else must prove it came
// from our own page.
func mutating(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

func (s *Server) sessionCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(s.TokenTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Off on a localhost dev setup with no TLS; DEPLOY.md requires it on
		// in production, where nginx terminates HTTPS.
		Secure: s.CookieSecure,
	}
}

func (s *Server) csrfCookie(value string) *http.Cookie {
	return &http.Cookie{
		Name:     CSRFCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   int(s.TokenTTL.Seconds()),
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.CookieSecure,
	}
}

// setSessionCookies is the login path: a fresh session and a fresh CSRF
// value together.
func (s *Server) setSessionCookies(w http.ResponseWriter, token string) {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	http.SetCookie(w, s.sessionCookie(token))
	http.SetCookie(w, s.csrfCookie(hex.EncodeToString(b)))
}

// refreshSessionCookies is the renewal path: a new token, and the SAME CSRF
// value with its clock wound back up. The value must not change mid-session
// — the page captured it once and echoes it on every request, and rotating
// it here would fail whatever requests are already in flight. It must be
// re-set all the same: renewal extends the session past the CSRF cookie's
// original Max-Age, and a session whose CSRF cookie has quietly expired can
// read everything but change nothing.
func (s *Server) refreshSessionCookies(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, s.sessionCookie(token))
	if c, err := r.Cookie(CSRFCookie); err == nil && c.Value != "" {
		http.SetCookie(w, s.csrfCookie(c.Value))
	}
}

// clearSessionCookies is logout. The token itself is not revoked — exactly
// the guarantee removing it from localStorage gave before, no weaker and no
// stronger; ending somebody's session server-side remains what
// /employees/{id}/revoke-sessions is for.
func (s *Server) clearSessionCookies(w http.ResponseWriter) {
	expire := func(name string, httpOnly bool) *http.Cookie {
		return &http.Cookie{
			Name: name, Value: "", Path: "/", MaxAge: -1,
			HttpOnly: httpOnly, SameSite: http.SameSiteLaxMode, Secure: s.CookieSecure,
		}
	}
	http.SetCookie(w, expire(SessionCookie, true))
	http.SetCookie(w, expire(CSRFCookie, false))
}

// csrfOK verifies the double-submit pair. Constant-time, though the value is
// not a secret in the cryptographic sense — the cost is one line.
func csrfOK(r *http.Request) bool {
	c, err := r.Cookie(CSRFCookie)
	if err != nil || c.Value == "" {
		return false
	}
	h := r.Header.Get(CSRFHeader)
	return h != "" && subtle.ConstantTimeCompare([]byte(h), []byte(c.Value)) == 1
}
