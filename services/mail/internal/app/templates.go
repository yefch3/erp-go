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
type TemplateInput struct {
	OwnerType string
	Name      string
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

// cleanTemplate is the validation and owner resolution both writing paths
// share, so an edit cannot store something a create would have refused.
// Mirrors cleanSignature deliberately — the two features carry the same
// risks for the same reasons.
func cleanTemplate(in TemplateInput, op Operator) (ownerType string, ownerID int64, content, format string, err error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Content) == "" {
		return "", 0, "", "", apierr.Invalid("NT_TEMPLATE_FIELDS_REQUIRED", "请填写模板名称和内容")
	}
	if !validTemplateLang(in.Lang) {
		// Refused rather than stored: the picker filters by this value, and
		// a row filed under an unknown tag would never be seen again.
		return "", 0, "", "", apierr.Invalid("NT_TEMPLATE_LANG_INVALID", "模板语言只支持中文、英文、西班牙文")
	}
	ownerType, ownerID = "EMPLOYEE", op.ID
	if strings.ToUpper(in.OwnerType) == "TENANT" {
		ownerType, ownerID = "TENANT", 0
	}
	format = normalizeFormat(in.Format)
	content = in.Content
	if format == FormatHTML {
		// Same rule as the body and the signature: sanitise on the way in,
		// because the stored value is both sent to customers and rendered
		// back into our own UI.
		content = SanitizeHTML(content)
		if blankSignature(content) {
			return "", 0, "", "", apierr.Invalid("NT_TEMPLATE_FIELDS_REQUIRED", "请填写模板名称和内容")
		}
	}
	return ownerType, ownerID, content, format, nil
}

func (s *Service) ListTemplates(ctx context.Context, tenantID int64, op Operator) ([]store.ListEmailTemplatesRow, error) {
	return s.q.ListEmailTemplates(ctx, store.ListEmailTemplatesParams{
		TenantID: tenantID, EmployeeID: op.ID,
	})
}

func (s *Service) CreateTemplate(ctx context.Context, tenantID int64, in TemplateInput, op Operator) (int64, error) {
	ownerType, ownerID, content, format, err := cleanTemplate(in, op)
	if err != nil {
		return 0, err
	}
	return s.q.CreateEmailTemplate(ctx, store.CreateEmailTemplateParams{
		TenantID: tenantID, OwnerType: ownerType, OwnerID: ownerID,
		Name: in.Name, Lang: in.Lang, Subject: in.Subject,
		Content: content, BodyFormat: format,
	})
}

func (s *Service) UpdateTemplate(ctx context.Context, tenantID, id int64, in TemplateInput, op Operator) error {
	ownerType, ownerID, content, format, err := cleanTemplate(in, op)
	if err != nil {
		return err
	}
	// Same sweep as signatures, same reason: a logo edited out of a template
	// is as orphaned as one whose template is gone.
	old, _ := s.q.GetEmailTemplate(ctx, store.GetEmailTemplateParams{TenantID: tenantID, ID: id})
	// The owner guard lives in the statement: a row the caller may not touch
	// simply matches nothing.
	n, err := s.q.UpdateEmailTemplate(ctx, store.UpdateEmailTemplateParams{
		TenantID: tenantID, ID: id, EmployeeID: op.ID,
		OwnerType: ownerType, OwnerID: ownerID,
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
	old, _ := s.q.GetEmailTemplate(ctx, store.GetEmailTemplateParams{TenantID: tenantID, ID: id})
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
