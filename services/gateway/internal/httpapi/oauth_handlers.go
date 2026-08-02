package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
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

func (u *UnlockStore) StoreOAuthState(ctx context.Context, state string, tenantID, employeeID int64) error {
	return u.rdb.Set(ctx, u.stateKey(state),
		fmt.Sprintf("%d:%d", tenantID, employeeID), 10*time.Minute).Err()
}

// TakeOAuthState consumes the state: single use, so a captured callback URL
// replayed later meets nothing.
func (u *UnlockStore) TakeOAuthState(ctx context.Context, state string) (int64, int64, bool) {
	v, err := u.rdb.GetDel(ctx, u.stateKey(state)).Result()
	if err != nil {
		return 0, 0, false
	}
	var tenantID, employeeID int64
	if _, err := fmt.Sscanf(v, "%d:%d", &tenantID, &employeeID); err != nil {
		return 0, 0, false
	}
	return tenantID, employeeID, true
}

// startGoogleOAuth hands the browser the door to Google's own login page.
func (s *Server) startGoogleOAuth(w http.ResponseWriter, r *http.Request) {
	if s.GoogleClientID == "" {
		s.writeError(w, http.StatusConflict, "OAUTH_NOT_CONFIGURED",
			"Google OAuth 未配置（缺少 GOOGLE_OAUTH_CLIENT_ID）")
		return
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		s.writeError(w, http.StatusInternalServerError, "OAUTH_STATE", "无法生成校验参数")
		return
	}
	state := hex.EncodeToString(b)
	op, _ := grpcx.OperatorFromContext(r.Context())
	if err := s.Unlock.StoreOAuthState(r.Context(), state, op.TenantID, op.EmployeeID); err != nil {
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
		"prompt":      {"consent"},
		"state":       {state},
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
	tenantID, employeeID, ok := s.Unlock.TakeOAuthState(r.Context(), state)
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
		Code: code, RedirectUri: s.OAuthRedirectURL,
	})
	if err != nil {
		back("oauth=err&reason=" + url.QueryEscape(grpcMessage(err)))
		return
	}
	back("oauth=ok&email=" + url.QueryEscape(resp.GetEmail()))
}
