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

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
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
func (s *Service) CompleteGoogleOAuth(ctx context.Context, tenantID, employeeID int64, code, redirectURI, expectEmail string) (string, error) {
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

	// 小写：地址一律以小写存（00046），而 ON CONFLICT (tenant_id, email) 是
	// 大小写敏感的。Google 通常回小写，但"通常"不是约束——大小写不一的那次
	// 会让同一个信箱裂成两行，两份凭据两份游标，而且不报错。
	email := strings.ToLower(strings.TrimSpace(emailFromIDToken(tok.IDToken)))
	if email == "" {
		return "", errors.New("无法从 Google 的应答中读出邮箱地址")
	}
	// expectEmail 只在调用方**说了**要绑哪一个时才比。
	//
	// 从前它一定是登录地址，因为那时「绑的必须是登录那个箱」。现在不是了：
	// ERP 账号是 263 的人，邮箱这边可以只绑 Gmail。留着这个参数是因为
	// 「重新授权某一个已绑的箱」仍然需要它——Google 每次都弹账号选择器，
	// 挑错一个就会把另一个箱的凭据覆盖掉，而两者长得一模一样。
	//
	// 在这里比而不是在网关比：等网关看到应答时凭据已经存进去了，存完再拒
	// 不叫拒。
	if want := strings.ToLower(strings.TrimSpace(expectEmail)); want != "" &&
		!strings.EqualFold(want, email) {
		return "", apierr.Invalid("MAIL_OAUTH_WRONG_ACCOUNT",
			fmt.Sprintf("你在 Google 授权的是 %s，但这一步要授权的是 %s。请选对账号。", email, want))
	}

	// 冲突键是**地址**：这个地址已经有行就是更新，没有就是新增一个信箱。
	//
	// 从前这里先按 employee_id 取一行当「上一个绑定」，地址不同就把那一行
	// 已同步的邮件全删掉——那是「一人一箱、换地址等于换箱」年代的清理。
	// 放开之后这段是**危险**的：按人取单行是 sqlc 的 :one，pgx 读到第一行
	// 就返回、不报错，于是绑第二个 Google 信箱时，它会拿「随便哪一行的旧
	// 地址」和新地址比，不同就删——而删的是本次刚 upsert 出来的那个 id。
	// 人绑完看到的是一个空信箱，日志里只有一行 Info。
	//
	// 现在不需要清理了：换一个地址是**新增一行**，新行本来就没有邮件；
	// 同一个地址重新授权是更新同一行，那些邮件正是它自己的。
	id, err := s.q.UpsertMailAccountShell(ctx, store.UpsertMailAccountShellParams{
		TenantID: tenantID, EmployeeID: employeeID, Email: email, Username: "",
	})
	if err != nil {
		return "", translateMailboxTaken(err)
	}
	// Google 的收发服务器种在行上。没有这一句，新绑的 Gmail 信箱主机是空的
	// ——发信和同步全停，而绑定这一步是成功的。
	if p, ok := MailProviderByCode("gmail"); ok {
		if err := s.q.SetMailAccountHosts(ctx, store.SetMailAccountHostsParams{
			TenantID: tenantID, ID: id, Domain: domainOf(email),
			SmtpHost: p.SMTPHost, SmtpPort: p.SMTPPort, SmtpSecurity: p.SMTPSecurity,
			ImapHost: p.IMAPHost, ImapPort: p.IMAPPort, ImapSecurity: p.IMAPSecurity,
		}); err != nil {
			return "", err
		}
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
	s.markVerified(ctx, tenantID, id)
	s.recordBinding(ctx, tenantID, employeeID, id, email, "gmail", bindActionBind, "")
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
		wrapped := fmt.Errorf("Google 授权已失效，请重新用 Google 登录绑定：%w", err)
		// 只有 invalid_grant 是"重新登录能修好"的：授权被撤销或过期。网络
		// 连不上、我们自己的 client_id 配错，重登都修不好，不打这个类型。
		if googleGrantRevoked(err) {
			return "", NewCredentialRejected(wrapped)
		}
		return "", wrapped
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
		return nil, googleOAuthError{code: tok.Error, desc: tok.ErrorDesc}
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

// googleOAuthError 是 Google 令牌接口明确说"不行"的那种应答，带着它的错误码。
// 和网络连不上、应答解析不了是两回事：后两种没有码。
type googleOAuthError struct{ code, desc string }

func (e googleOAuthError) Error() string {
	return fmt.Sprintf("Google 拒绝了请求：%s（%s）", e.code, e.desc)
}

// googleGrantRevoked 说这次失败是不是"授权本身没了"。
//
// Google 用 invalid_grant 表示 refresh token 被撤销、过期或已经换过密码。
// 这是唯一一种用户重新走一遍 Google 登录就能修好的情况；invalid_client
// 之类是我们自己的配置问题，劝用户重登只会让他白跑。
func googleGrantRevoked(err error) bool {
	var g googleOAuthError
	return errors.As(err, &g) && g.code == "invalid_grant"
}
