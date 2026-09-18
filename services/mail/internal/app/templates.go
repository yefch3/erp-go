package app

import (
	"context"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// TemplateInput is one reusable phrase block (C5 邮件模板库).
//
// Templates carry {{variables}} from the same vocabulary campaigns render
// with (see render.go KnownVariables) — one vocabulary for the whole
// service, or the same tag would resolve in a bulk send and sit unresolved
// in a single mail.
//
// No owner in here, for the same reason SignatureInput has none: a template
// belongs to whoever writes it (2026-09-18). The company phrasebook that used
// to sit beside the personal layer is gone.
type TemplateInput struct {
	Name string
	// zh / en / es, or "" for a template that reads the same to everyone.
	// A filter for the picker, not a grouping key.
	Lang    string
	Subject string
	Content string
	// TEXT or HTML.
	Format string
}

func validTemplateLang(lang string) bool {
	switch lang {
	case "", "zh", "en", "es":
		return true
	}
	return false
}

// cleanTemplate is the validation both writing paths share, so an edit
// cannot store something a create would have refused. Mirrors
// cleanSignature deliberately — the two features carry the same risks for
// the same reasons.
func cleanTemplate(in TemplateInput) (content, format string, err error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Content) == "" {
		return "", "", apierr.Invalid("NT_TEMPLATE_FIELDS_REQUIRED", "请填写模板名称和内容")
	}
	if !validTemplateLang(in.Lang) {
		// Refused rather than stored: the picker filters by this value, and
		// a row filed under an unknown tag would never be seen again.
		return "", "", apierr.Invalid("NT_TEMPLATE_LANG_INVALID", "模板语言只支持中文、英文、西班牙文")
	}
	format = normalizeFormat(in.Format)
	content = in.Content
	if format == FormatHTML {
		// Same rule as the body and the signature: sanitise on the way in,
		// because the stored value is both sent to customers and rendered
		// back into our own UI.
		content = SanitizeHTML(content)
		if blankSignature(content) {
			return "", "", apierr.Invalid("NT_TEMPLATE_FIELDS_REQUIRED", "请填写模板名称和内容")
		}
	}
	return content, format, nil
}

func (s *Service) ListTemplates(ctx context.Context, tenantID int64, op Operator) ([]store.ListEmailTemplatesRow, error) {
	return s.q.ListEmailTemplates(ctx, store.ListEmailTemplatesParams{
		TenantID: tenantID, EmployeeID: op.ID,
	})
}

func (s *Service) CreateTemplate(ctx context.Context, tenantID int64, in TemplateInput, op Operator) (int64, error) {
	content, format, err := cleanTemplate(in)
	if err != nil {
		return 0, err
	}
	return s.q.CreateEmailTemplate(ctx, store.CreateEmailTemplateParams{
		TenantID: tenantID, OwnerID: op.ID,
		Name: in.Name, Lang: in.Lang, Subject: in.Subject,
		Content: content, BodyFormat: format,
	})
}

func (s *Service) UpdateTemplate(ctx context.Context, tenantID, id int64, in TemplateInput, op Operator) error {
	content, format, err := cleanTemplate(in)
	if err != nil {
		return err
	}
	// Same sweep as signatures, same reason: a logo edited out of a template
	// is as orphaned as one whose template is gone.
	old, _ := s.q.GetEmailTemplate(ctx, store.GetEmailTemplateParams{TenantID: tenantID, ID: id, EmployeeID: op.ID})
	// The owner guard lives in the statement: a row the caller may not touch
	// simply matches nothing.
	n, err := s.q.UpdateEmailTemplate(ctx, store.UpdateEmailTemplateParams{
		TenantID: tenantID, ID: id, EmployeeID: op.ID,
		Name: in.Name, Lang: in.Lang, Subject: in.Subject,
		Content: content, BodyFormat: format,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("NT_TEMPLATE_NOT_FOUND", "模板不存在")
	}
	s.sweepRemovedImages(ctx, tenantID, old.Content, content)
	return nil
}

func (s *Service) DeleteTemplate(ctx context.Context, tenantID, id int64, op Operator) error {
	old, _ := s.q.GetEmailTemplate(ctx, store.GetEmailTemplateParams{TenantID: tenantID, ID: id, EmployeeID: op.ID})
	n, err := s.q.DeleteEmailTemplate(ctx, store.DeleteEmailTemplateParams{
		TenantID: tenantID, ID: id, EmployeeID: op.ID,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("NT_TEMPLATE_NOT_FOUND", "模板不存在")
	}
	s.sweepRemovedImages(ctx, tenantID, old.Content, "")
	return nil
}
