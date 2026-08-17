package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

// The OAuth state parameter, stored server-side.
//
// State is what stops a forged callback: Google echoes it back untouched, and
// only a value we minted minutes ago for a known person is accepted. It also
// carries the identity across the redirect — the browser comes back from
// Google without our Authorization header, so the state is the only thread
// connecting the callback to the employee who started it.

func (u *UnlockStore) stateKey(state string) string {
	return "erp.oauthstate." + state
}

// StoreOAuthState remembers who left for Google, and which address they are
// allowed to come back with.
//
// The address is carried here because the callback arrives as a bare browser
// redirect with no token on it — by then the only thing tying the request to a
// person is this state. Looking it up again from iam would work too; keeping
// it with the state means the value cannot have changed in between.
func (u *UnlockStore) StoreOAuthState(ctx context.Context, state string, tenantID, employeeID int64, email string) error {
	return u.rdb.Set(ctx, u.stateKey(state),
		fmt.Sprintf("%d:%d:%s", tenantID, employeeID, email), 10*time.Minute).Err()
}

// TakeOAuthState consumes the state: single use, so a captured callback URL
// replayed later meets nothing.
func (u *UnlockStore) TakeOAuthState(ctx context.Context, state string) (int64, int64, string, bool) {
	v, err := u.rdb.GetDel(ctx, u.stateKey(state)).Result()
	if err != nil {
		return 0, 0, "", false
	}
	parts := strings.SplitN(v, ":", 3)
	if len(parts) < 2 {
		return 0, 0, "", false
	}
	tenantID, err1 := strconv.ParseInt(parts[0], 10, 64)
	employeeID, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, "", false
	}
	email := ""
	if len(parts) == 3 {
		email = parts[2]
	}
	return tenantID, employeeID, email, true
}

// googleWillRefuse reports whether Google would reject this redirect URI
// before we send anybody to it, and says why in words the person can act on.
//
// Google's OAuth 2.0 policy takes only https:// on a real domain name; the
// single exception is localhost, which may use http. A deployment reached by
// bare IP — which is exactly what this system is between moving to the cloud
// and buying a domain — therefore cannot use the Google button at all.
//
// Checked here rather than left to Google, because Google's refusal is a page
// titled "Access blocked: Authorization Error / Error 400: invalid_request"
// which names neither the cause nor the alternative. Ours does both, and the
// alternative genuinely works: 密码/授权码 login binds the same mailbox.
func googleWillRefuse(redirect string) string {
	u, err := url.Parse(redirect)
	if err != nil || u.Host == "" {
		return "回调地址无效，请联系管理员检查 MAIL_OAUTH_REDIRECT_URL"
	}
	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" {
		return ""
	}
	const useThePasswordPath = "请改用下方的「邮箱密码 / 客户端授权码」登录（Gmail 请使用应用专用密码）。"
	// The IP case is judged first and its message names BOTH requirements on
	// purpose: a bare IP over https is still refused, so telling somebody
	// only "needs https" would send them to buy a certificate for an address
	// that can never work. Getting a domain is what unlocks both.
	if net.ParseIP(host) != nil {
		return "Google 登录要求使用域名（且必须是 https），当前系统还在用 IP 地址访问。" + useThePasswordPath
	}
	if u.Scheme != "https" {
		return "Google 登录要求 https 地址，当前系统还在用 http 访问。" + useThePasswordPath
	}
	return ""
}

// startGoogleOAuth hands the browser the door to Google's own login page.
func (s *Server) startGoogleOAuth(w http.ResponseWriter, r *http.Request) {
	if s.GoogleClientID == "" {
		s.writeError(w, http.StatusConflict, "OAUTH_NOT_CONFIGURED",
			"Google OAuth 未配置（缺少 GOOGLE_OAUTH_CLIENT_ID）")
		return
	}
	if why := googleWillRefuse(s.OAuthRedirectURL); why != "" {
		s.writeError(w, http.StatusConflict, "OAUTH_UNAVAILABLE_HERE", why)
		return
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		s.writeError(w, http.StatusInternalServerError, "OAUTH_STATE", "无法生成校验参数")
		return
	}
	state := hex.EncodeToString(b)
	op, _ := grpcx.OperatorFromContext(r.Context())
	if err := s.Unlock.StoreOAuthState(r.Context(), state, op.TenantID, op.EmployeeID, op.Email); err != nil {
		s.writeError(w, http.StatusInternalServerError, "OAUTH_STATE", "无法保存校验参数")
		return
	}

	q := url.Values{
		"client_id":     {s.GoogleClientID},
		"redirect_uri":  {s.OAuthRedirectURL},
		"response_type": {"code"},
		// openid email names the mailbox being granted; mail.google.com is
		// the IMAP/SMTP grant itself.
		"scope": {"openid email https://mail.google.com/"},
		// offline + consent is what guarantees a refresh token on every pass,
		// not only the first — without it a rebind quietly comes back
		// tokenless and fails an hour later.
		"access_type": {"offline"},
		// select_account as well as consent, so the account chooser appears
		// every time rather than silently reusing whichever Google account the
		// browser happens to be signed in to. Signing in to a mailbox is not
		// something to do by accident, and there is no longer a separate
		// "switch account" link — this is it.
		"prompt": {"select_account consent"},
		// ...and the right account is pre-selected, because only one is
		// acceptable. The chooser is there so the person sees which mailbox
		// they are opening, not so they can pick a different one; picking a
		// different one is refused on the way back.
		"login_hint": {op.Email},
		"state":      {state},
	}
	writeUnlockJSON(w, map[string]any{
		"url": "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode(),
	})
}

// googleOAuthCallback is where Google sends the browser back.
//
// Login-free by necessity: this is a top-level redirect and our JWT lives in
// localStorage, not cookies. The state parameter is the sole authentication,
// which is exactly the job it exists for.
func (s *Server) googleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	// Back to the mailbox itself: the sign-in surface lives on the 邮箱 page
	// now, and on success the page verifies the fresh grant and walks straight
	// through the gate.
	back := func(query string) {
		http.Redirect(w, r, s.FrontendBaseURL+"/emails?"+query, http.StatusFound)
	}
	if e := r.URL.Query().Get("error"); e != "" {
		// The person clicked cancel on Google's page, or Google refused.
		back("oauth=err&reason=" + url.QueryEscape(e))
		return
	}
	state, code := r.URL.Query().Get("state"), r.URL.Query().Get("code")
	tenantID, employeeID, wantEmail, ok := s.Unlock.TakeOAuthState(r.Context(), state)
	if !ok {
		back("oauth=err&reason=" + url.QueryEscape("授权状态已过期或不合法，请重试"))
		return
	}

	// The callback carries no JWT, so the operator context is rebuilt from
	// the state we minted — the one thing that ties this browser to a person.
	ctx := grpcx.WithOperator(r.Context(), grpcx.Operator{
		TenantID: tenantID, EmployeeID: employeeID,
		IP: r.RemoteAddr, TraceID: newTraceID(),
	})
	resp, err := s.Emails.CompleteGoogleOAuth(ctx, &mailv1.CompleteGoogleOAuthRequest{
		Code: code, RedirectUri: s.OAuthRedirectURL, ExpectEmail: wantEmail,
	})
	if err != nil {
		back("oauth=err&reason=" + url.QueryEscape(grpcMessage(err)))
		return
	}
	back("oauth=ok&email=" + url.QueryEscape(resp.GetEmail()))
}
