package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sgao19/erp-go/services/notification/internal/store"
)

// OAuthConfig carries the Google client credentials. The secret identifies
// our application to Google, not any user to anything — but it still only
// travels environment → memory, never a log line or an API response.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
}

// UseOAuth installs the Google client credentials after construction.
func (s *Service) UseOAuth(cfg OAuthConfig) { s.oauth = cfg }

const googleTokenURL = "https://oauth2.googleapis.com/token"

type googleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// CompleteGoogleOAuth turns an authorisation code into a bound mailbox.
//
// The person just proved who they are on Google's own page; what comes back
// is a refresh token — a standing, revocable grant to act on their mailbox.
// Their password was typed at Google and never existed here.
func (s *Service) CompleteGoogleOAuth(ctx context.Context, tenantID, employeeID int64, code, redirectURI string) (string, error) {
	if s.oauth.ClientID == "" || s.oauth.ClientSecret == "" {
		return "", errors.New("Google OAuth 未配置（缺少 GOOGLE_OAUTH_CLIENT_ID / SECRET）")
	}
	if s.secrets == nil {
		return "", ErrNoKey
	}

	tok, err := s.googleToken(ctx, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
	})
	if err != nil {
		return "", err
	}
	if tok.RefreshToken == "" {
		// Happens when consent was granted before without offline access.
		// The start URL always asks with prompt=consent, so reaching this
		// means something upstream changed the URL.
		return "", errors.New("Google 没有返回长期授权（refresh token），请重试一次登录")
	}

	email := emailFromIDToken(tok.IDToken)
	if email == "" {
		return "", errors.New("无法从 Google 的应答中读出邮箱地址")
	}

	// Bind. Rebinding to a different address makes every stored message and
	// UID meaningless, so the old mailbox's data goes with the old binding.
	prev, prevErr := s.q.GetMyMailAccount(ctx, store.GetMyMailAccountParams{
		TenantID: tenantID, EmployeeID: employeeID,
	})
	id, err := s.q.UpsertMailAccountShell(ctx, store.UpsertMailAccountShellParams{
		TenantID: tenantID, EmployeeID: employeeID, Email: email, Username: "",
	})
	if err != nil {
		return "", err
	}
	if prevErr == nil && prev.Email != "" && !strings.EqualFold(prev.Email, email) {
		s.log.Info("mailbox rebound to a different address, clearing its synced mail",
			"employee", employeeID, "was", prev.Email, "now", email)
		_ = s.q.DeleteInboundForAccount(ctx, store.DeleteInboundForAccountParams{TenantID: tenantID, AccountID: id})
		_ = s.q.DeleteSyncStateForAccount(ctx, store.DeleteSyncStateForAccountParams{TenantID: tenantID, AccountID: id})
		s.sentFolders.Delete(id)
	}

	blob, err := s.secrets.Seal([]byte(tok.RefreshToken), OAuthAAD(tenantID, id))
	if err != nil {
		return "", err
	}
	if err := s.q.SetMailAccountOAuth(ctx, store.SetMailAccountOAuthParams{
		TenantID: tenantID, ID: id, OauthRefreshEnc: blob, KeyVersion: int32(s.secrets.Version()),
	}); err != nil {
		return "", err
	}
	// The fresh access token is already in hand; caching it saves the first
	// sync a round trip to Google.
	s.tokenCache.Store(id, cachedToken{
		token: tok.AccessToken,
		exp:   time.Now().Add(time.Duration(tok.ExpiresIn-60) * time.Second),
	})
	return email, nil
}

// OAuthAAD binds a refresh-token blob to its exact row, same reasoning as
// AccountAAD — and a different prefix, so the two columns' blobs can never be
// swapped for each other.
func OAuthAAD(tenantID, accountID int64) []byte {
	return []byte(fmt.Sprintf("mail_oauth:%d:%d", tenantID, accountID))
}

type cachedToken struct {
	token string
	exp   time.Time
}

// accessTokenFor turns the stored refresh token into a live access token,
// with an in-memory cache: Google's tokens last an hour, and asking for a
// fresh one per message would be sixty round trips for nothing.
func (s *Service) accessTokenFor(ctx context.Context, tenantID, accountID int64, refreshEnc []byte) (string, error) {
	if v, ok := s.tokenCache.Load(accountID); ok {
		c := v.(cachedToken)
		if time.Now().Before(c.exp) {
			return c.token, nil
		}
	}
	refresh, err := s.secrets.Open(refreshEnc, OAuthAAD(tenantID, accountID))
	if err != nil {
		return "", errors.New("已保存的 Google 授权无法解密，请重新用 Google 登录绑定")
	}
	tok, err := s.googleToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {string(refresh)},
	})
	if err != nil {
		// The grant itself may have been revoked from the Google account's
		// security page. Say what to do, not just what failed.
		return "", fmt.Errorf("Google 授权已失效，请重新用 Google 登录绑定：%w", err)
	}
	s.tokenCache.Store(accountID, cachedToken{
		token: tok.AccessToken,
		exp:   time.Now().Add(time.Duration(tok.ExpiresIn-60) * time.Second),
	})
	return tok.AccessToken, nil
}

func (s *Service) googleToken(ctx context.Context, form url.Values) (*googleTokenResponse, error) {
	form.Set("client_id", s.oauth.ClientID)
	form.Set("client_secret", s.oauth.ClientSecret)

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 Google 授权服务器：%w", err)
	}
	defer resp.Body.Close()

	var tok googleTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, fmt.Errorf("Google 应答无法解析：%w", err)
	}
	if tok.Error != "" {
		return nil, fmt.Errorf("Google 拒绝了请求：%s（%s）", tok.Error, tok.ErrorDesc)
	}
	if tok.AccessToken == "" {
		return nil, errors.New("Google 没有返回访问令牌")
	}
	return &tok, nil
}

// emailFromIDToken reads the address out of an OpenID id_token.
//
// The payload is decoded without signature verification, which is correct
// here and only here: this token arrived directly from Google's token
// endpoint over TLS in exchange for our client secret. Signatures exist for
// tokens that travelled through untrusted hands; this one never did.
func emailFromIDToken(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(claims.Email))
}
