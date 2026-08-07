package app

import (
	"context"
	"errors"
	"strings"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// MailHostSettings is the tenant's connection configuration. No secrets, so
// it can be read back to a browser freely.
type MailHostSettings struct {
	Domain       string
	SMTPHost     string
	SMTPPort     int32
	SMTPSecurity string
	IMAPHost     string
	IMAPPort     int32
	IMAPSecurity string
	HourlyQuota  int32
	DailyQuota   int32
}

// MailAccountView is what the settings page shows about somebody's own
// mailbox. HasSecret rather than the secret: a value that is never sent to a
// browser cannot be leaked by one.
type MailAccountView struct {
	Email      string
	Username   string
	AuthKind   string
	HasSecret  bool
	VerifiedAt string
	LastError  string
	IsActive   bool
}

var validSecurity = map[string]bool{"SSL": true, "STARTTLS": true, "NONE": true}

// normaliseDomain accepts what people actually type.
//
// A field labelled "domain" invites a whole address, and the only visible
// consequence of one is a malformed Message-ID like <key@lina@sunrise.com>,
// which nobody notices until deliverability quietly suffers. Fixing the input
// is cheaper than rejecting it.
func normaliseDomain(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func (s *Service) GetMailHost(ctx context.Context, tenantID int64) (MailHostSettings, error) {
	row, err := s.q.GetMailHost(ctx, tenantID)
	if err != nil {
		// Nothing configured yet is a normal state, not an error: the
		// settings page has to render before anybody can fill it in.
		return MailHostSettings{
			SMTPPort: 465, SMTPSecurity: "SSL",
			IMAPPort: 993, IMAPSecurity: "SSL",
			HourlyQuota: 100, DailyQuota: 500,
		}, nil
	}
	return MailHostSettings{
		Domain: row.Domain, SMTPHost: row.SmtpHost, SMTPPort: row.SmtpPort,
		SMTPSecurity: row.SmtpSecurity, IMAPHost: row.ImapHost, IMAPPort: row.ImapPort,
		IMAPSecurity: row.ImapSecurity, HourlyQuota: row.HourlyQuota, DailyQuota: row.DailyQuota,
	}, nil
}

func (s *Service) SaveMailHost(ctx context.Context, tenantID int64, in MailHostSettings) error {
	in.SMTPSecurity = strings.ToUpper(strings.TrimSpace(in.SMTPSecurity))
	in.IMAPSecurity = strings.ToUpper(strings.TrimSpace(in.IMAPSecurity))
	if !validSecurity[in.SMTPSecurity] || !validSecurity[in.IMAPSecurity] {
		return errors.New("加密方式只能是 SSL、STARTTLS 或 NONE")
	}
	if in.SMTPHost == "" {
		return errors.New("请填写 SMTP 服务器地址")
	}
	if in.SMTPPort <= 0 || in.SMTPPort > 65535 || in.IMAPPort <= 0 || in.IMAPPort > 65535 {
		return errors.New("端口必须在 1 到 65535 之间")
	}
	if in.HourlyQuota <= 0 || in.DailyQuota <= 0 {
		return errors.New("发送上限必须大于 0")
	}
	in.Domain = normaliseDomain(in.Domain)
	return s.q.UpsertMailHost(ctx, store.UpsertMailHostParams{
		TenantID: tenantID, Domain: in.Domain,
		SmtpHost: in.SMTPHost, SmtpPort: in.SMTPPort, SmtpSecurity: in.SMTPSecurity,
		ImapHost: in.IMAPHost, ImapPort: in.IMAPPort, ImapSecurity: in.IMAPSecurity,
		HourlyQuota: in.HourlyQuota, DailyQuota: in.DailyQuota,
	})
}

func (s *Service) GetMyMailAccount(ctx context.Context, tenantID, employeeID int64) (MailAccountView, error) {
	row, err := s.q.GetMyMailAccount(ctx, store.GetMyMailAccountParams{
		TenantID: tenantID, EmployeeID: employeeID,
	})
	if err != nil {
		// Not yet configured. Same reasoning as the host above.
		return MailAccountView{IsActive: true}, nil
	}
	// HasSecret is derived from a separate read rather than selected above,
	// because GetMyMailAccount deliberately does not touch the secret column
	// at all — keeping that query incapable of returning it is worth one
	// extra round trip on a settings page.
	//
	// Both columns, because a credential lives in one or the other and never
	// both: a password account fills secret_enc, a Google account fills
	// oauth_refresh_enc and leaves secret_enc empty. Looking only at the first
	// answers "no credential" for every Google-signed-in employee in the
	// company — which reads as "you have not set up your mailbox" to somebody
	// whose mailbox is working.
	has := false
	if sec, err := s.q.GetMailAccountSecret(ctx, store.GetMailAccountSecretParams{
		TenantID: tenantID, EmployeeID: employeeID,
	}); err == nil {
		has = len(sec.SecretEnc) > 0 || len(sec.OauthRefreshEnc) > 0
	}

	v := MailAccountView{
		Email: row.Email, Username: row.Username, AuthKind: row.AuthKind,
		HasSecret: has, LastError: row.LastError, IsActive: row.IsActive,
	}
	if row.VerifiedAt.Valid {
		v.VerifiedAt = row.VerifiedAt.Time.Format("2006-01-02 15:04")
	}
	return v, nil
}
