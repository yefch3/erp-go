package app

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// CampaignQuery is the bulk-send list filter.
type CampaignQuery struct {
	Keyword    string
	SenderID   int64
	Page, Size int32
}

func (s *Service) ListCampaigns(ctx context.Context, tenantID int64, qy CampaignQuery, op Operator) ([]store.ListCampaignsRow, int64, error) {
	visible, err := s.visibleTo(ctx, op.ID)
	if err != nil {
		return nil, 0, err
	}
	page, size := normalizePage(qy.Page, qy.Size)
	rows, err := s.q.ListCampaigns(ctx, store.ListCampaignsParams{
		TenantID: tenantID, VisibleAll: visible.All, VisibleIds: visible.EmployeeIDs,
		SenderID: qy.SenderID,
		Keyword:  qy.Keyword, RowLimit: size, RowOffset: (page - 1) * size,
	})
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	}
	return rows, total, nil
}

// GetCampaign reads one bulk send with the templates as composed.
func (s *Service) GetCampaign(ctx context.Context, tenantID, id int64, op Operator) (store.GetCampaignRow, error) {
	c, err := s.q.GetCampaign(ctx, store.GetCampaignParams{TenantID: tenantID, ID: id})
	if err == pgx.ErrNoRows {
		return store.GetCampaignRow{}, apierr.NotFound("NT_CAMPAIGN_NOT_FOUND", "群发记录不存在")
	}
	if err != nil {
		return store.GetCampaignRow{}, err
	}
	if err := s.mayRead(ctx, op.ID, c.SenderID); err != nil {
		return store.GetCampaignRow{}, err
	}
	return c, nil
}

// ListSenders answers "whose mailbox may I open" for the supervisor view.
//
// Derived from the same data scope as everything else, so a supervisor who
// gains or loses a report sees the picker change without anybody editing a
// second list.
func (s *Service) ListSenders(ctx context.Context, tenantID int64, op Operator) ([]store.ListSendersWithCountsRow, error) {
	visible, err := s.visibleTo(ctx, op.ID)
	if err != nil {
		return nil, err
	}
	return s.q.ListSendersWithCounts(ctx, store.ListSendersWithCountsParams{
		TenantID: tenantID, VisibleAll: visible.All, VisibleIds: visible.EmployeeIDs,
	})
}

// MessageQuery is the per-recipient list filter.
type MessageQuery struct {
	CampaignID int64
	SenderID   int64
	Status     string
	Keyword    string
	// The single filter the failure page needs.
	AttentionOnly bool
	// Where the previous page ended: the id of its last row, 0 for the first.
	// Keyset rather than a page number, like every other mailbox list.
	Cursor string
	Size   int32
}

// ListMessages is what both the campaign detail and the failure queue read.
//
// The data scope applies here too: a supervisor with DEPT sees their team's
// correspondence, everybody else sees their own. Reading somebody else's mail
// is a real act, so it is gated like one.
func (s *Service) ListMessages(ctx context.Context, tenantID int64, qy MessageQuery, op Operator) ([]store.ListMessagesRow, int64, string, error) {
	visible, err := s.visibleTo(ctx, op.ID)
	if err != nil {
		return nil, 0, "", err
	}
	_, size := normalizePage(1, qy.Size)
	cursorID, err := decodeIDCursor(qy.Cursor)
	if err != nil {
		return nil, 0, "", err
	}
	rows, err := s.q.ListMessages(ctx, store.ListMessagesParams{
		TenantID: tenantID, VisibleAll: visible.All, VisibleIds: visible.EmployeeIDs,
		CampaignID: qy.CampaignID, SenderID: qy.SenderID,
		Status: qy.Status, AttentionOnly: qy.AttentionOnly,
		Keyword: qy.Keyword, RowLimit: size, CursorID: cursorID,
	})
	if err != nil {
		return nil, 0, "", err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	}
	next := ""
	if int32(len(rows)) == size && size > 0 {
		next = encodeIDCursor(rows[len(rows)-1].ID)
	}
	return rows, total, next, nil
}

// A cursor over a list ordered by id alone. Encoded rather than passed as a
// bare number so it reads as a position, not as something to do arithmetic on.
func encodeIDCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}

func decodeIDCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, errBadCursor()
	}
	id, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return 0, errBadCursor()
	}
	return id, nil
}

// MessageView is one recipient's mail with its delivery history.
type MessageView struct {
	Message store.GetMessageRow
	Events  []store.ListEventsOfMessageRow
	// Whether this mail actually carried an open-tracking pixel. Without it,
	// "never opened" and "never watched" are the same empty field, and only
	// one of them is a statement about the recipient.
	TrackingEnabled bool
}

func (s *Service) GetMessage(ctx context.Context, tenantID, id int64, op Operator) (MessageView, error) {
	m, err := s.q.GetMessage(ctx, store.GetMessageParams{TenantID: tenantID, ID: id})
	if err == pgx.ErrNoRows {
		return MessageView{}, apierr.NotFound("NT_MESSAGE_NOT_FOUND", "邮件记录不存在")
	}
	if err != nil {
		return MessageView{}, err
	}
	// Reading somebody else's mail is a real act, so it is gated like one —
	// including on the detail view, which is the one that shows the body.
	if err := s.mayRead(ctx, op.ID, m.SenderID); err != nil {
		return MessageView{}, err
	}
	events, err := s.q.ListEventsOfMessage(ctx, store.ListEventsOfMessageParams{
		TenantID: tenantID, MessageID: id,
	})
	if err != nil {
		return MessageView{}, err
	}
	// Read from the row, not sniffed from the body. The pixel is injected at
	// send time into a copy — so that what is stored stays what the person
	// wrote — which means the stored body never contains it and looking there
	// would report every mail as untracked.
	return MessageView{Message: m, Events: events, TrackingEnabled: m.Tracked}, nil
}

// Requeue puts a failed message back, optionally at a corrected address.
//
// A hard-bounced address is also lifted from the suppression list when it is
// being replaced: the old address stays blocked, the new one deserves a
// chance, and leaving the correction blocked would make the fix look broken.
func (s *Service) Requeue(ctx context.Context, tenantID, id int64, newEmail string, op Operator) error {
	if err := s.mayActOn(ctx, tenantID, id, op); err != nil {
		return err
	}
	newEmail = strings.ToLower(strings.TrimSpace(newEmail))
	if newEmail != "" {
		if _, err := s.q.RemoveSuppression(ctx, store.RemoveSuppressionParams{
			TenantID: tenantID, Email: newEmail,
		}); err != nil {
			return err
		}
	}
	n, err := s.q.RequeueMessage(ctx, store.RequeueMessageParams{
		TenantID: tenantID, ID: id, ToEmail: newEmail,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.Invalid("NT_MESSAGE_NOT_RETRYABLE",
			"这封邮件当前状态不能重发")
	}
	s.log.Info("message requeued by hand", "id", id, "by", op.Name, "new_address", newEmail != "")
	return nil
}

// Abandon closes a failed message without sending it.
func (s *Service) Abandon(ctx context.Context, tenantID, id int64, reason string, op Operator) error {
	if strings.TrimSpace(reason) == "" {
		return apierr.Invalid("NT_ABANDON_REASON_REQUIRED", "请填写放弃原因")
	}
	if err := s.mayActOn(ctx, tenantID, id, op); err != nil {
		return err
	}
	n, err := s.q.AbandonMessage(ctx, store.AbandonMessageParams{
		TenantID: tenantID, ID: id, Reason: reason,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.Invalid("NT_MESSAGE_NOT_ABANDONABLE", "这封邮件当前状态不能放弃")
	}
	return nil
}

// mayActOn gates the two corrective actions the failure queue offers. Both
// change what a customer receives, so the same scope that decides who may
// read a mail decides who may resend or drop it.
func (s *Service) mayActOn(ctx context.Context, tenantID, id int64, op Operator) error {
	m, err := s.q.GetMessage(ctx, store.GetMessageParams{TenantID: tenantID, ID: id})
	if err == pgx.ErrNoRows {
		return apierr.NotFound("NT_MESSAGE_NOT_FOUND", "邮件记录不存在")
	}
	if err != nil {
		return err
	}
	return s.mayRead(ctx, op.ID, m.SenderID)
}

// ---------------------------------------------------------------- signatures

// SignatureInput is one signature block.
type SignatureInput struct {
	OwnerType string
	Name      string
	Content   string
	// TEXT or HTML. HTML is what allows a logo in the sign-off.
	Format    string
	IsDefault bool
}

func (s *Service) ListSignatures(ctx context.Context, tenantID int64, op Operator) ([]store.ListSignaturesRow, error) {
	return s.q.ListSignatures(ctx, store.ListSignaturesParams{
		TenantID: tenantID, EmployeeID: op.ID,
	})
}

// cleanSignature is the validation and owner resolution both writing paths
// share, so an edit cannot store something a create would have refused.
func cleanSignature(in SignatureInput, op Operator) (ownerType string, ownerID int64, content, format string, err error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Content) == "" {
		return "", 0, "", "", apierr.Invalid("NT_SIGNATURE_FIELDS_REQUIRED", "请填写签名名称和内容")
	}
	ownerType, ownerID = "EMPLOYEE", op.ID
	if strings.ToUpper(in.OwnerType) == "TENANT" {
		ownerType, ownerID = "TENANT", 0
	}
	format = normalizeFormat(in.Format)
	content = in.Content
	if format == FormatHTML {
		// Same rule as the body: sanitise on the way in, because the stored
		// value is both sent to customers and rendered back in the UI.
		content = SanitizeHTML(content)
		if blankSignature(content) {
			// A rich editor left alone does not produce an empty string, it
			// produces "<br>" or an empty paragraph — which passes the check
			// above and stores a signature that appends nothing to every mail
			// and looks, in the list, exactly like a working one.
			return "", 0, "", "", apierr.Invalid("NT_SIGNATURE_FIELDS_REQUIRED", "请填写签名名称和内容")
		}
	}
	return ownerType, ownerID, content, format, nil
}

func (s *Service) CreateSignature(ctx context.Context, tenantID int64, in SignatureInput, op Operator) (int64, error) {
	ownerType, ownerID, content, format, err := cleanSignature(in, op)
	if err != nil {
		return 0, err
	}
	var id int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if in.IsDefault {
			// Move the flag before setting the new one: the partial unique
			// index would otherwise refuse the insert, and a constraint
			// violation reads far worse than simply switching defaults.
			//
			// In a transaction with the insert, so a failure here cannot leave
			// the owner with no default at all — which is what "clear, then
			// fail" produced when these were two separate statements.
			if err := q.ClearDefaultSignature(ctx, store.ClearDefaultSignatureParams{
				TenantID: tenantID, OwnerType: ownerType, OwnerID: ownerID,
			}); err != nil {
				return err
			}
		}
		id, err = q.CreateSignature(ctx, store.CreateSignatureParams{
			TenantID: tenantID, OwnerType: ownerType, OwnerID: ownerID,
			Name: in.Name, Content: content, BodyFormat: format,
			IsDefault: in.IsDefault,
		})
		return err
	})
	return id, err
}

// UpdateSignature rewrites a block in place.
//
// In place, rather than delete-and-recreate: a campaign row points at a
// signature by id, and recreating would orphan every send that referenced it.
func (s *Service) UpdateSignature(ctx context.Context, tenantID, id int64, in SignatureInput, op Operator) error {
	ownerType, ownerID, content, format, err := cleanSignature(in, op)
	if err != nil {
		return err
	}
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if in.IsDefault {
			if err := q.ClearDefaultSignature(ctx, store.ClearDefaultSignatureParams{
				TenantID: tenantID, OwnerType: ownerType, OwnerID: ownerID,
			}); err != nil {
				return err
			}
		}
		// The owner guard lives in the statement, so a row the caller may not
		// touch simply matches nothing. Rolling back means an edit refused
		// this way cannot have cleared somebody else's default on its way out.
		n, err := q.UpdateSignature(ctx, store.UpdateSignatureParams{
			TenantID: tenantID, ID: id, EmployeeID: op.ID,
			OwnerType: ownerType, OwnerID: ownerID,
			Name: in.Name, Content: content, BodyFormat: format,
			IsDefault: in.IsDefault,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.NotFound("NT_SIGNATURE_NOT_FOUND", "签名不存在")
		}
		return nil
	})
}

// blankSignature reports markup that renders as nothing.
//
// An image alone is not blank: a sign-off that is only the company logo is a
// perfectly ordinary signature, and judging by text would refuse it.
func blankSignature(html string) bool {
	if strings.Contains(strings.ToLower(html), "<img") {
		return false
	}
	return strings.TrimSpace(HTMLToText(html)) == ""
}

func (s *Service) DeleteSignature(ctx context.Context, tenantID, id int64, op Operator) error {
	n, err := s.q.DeleteSignature(ctx, store.DeleteSignatureParams{
		TenantID: tenantID, ID: id, EmployeeID: op.ID,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("NT_SIGNATURE_NOT_FOUND", "签名不存在")
	}
	return nil
}

// ---------------------------------------------------------------- suppression

func (s *Service) ListSuppressions(ctx context.Context, tenantID int64, keyword string) ([]store.ListSuppressionsRow, error) {
	return s.q.ListSuppressions(ctx, store.ListSuppressionsParams{
		TenantID: tenantID, Keyword: keyword, RowLimit: 200,
	})
}

func (s *Service) Suppress(ctx context.Context, tenantID int64, email, reason, detail string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return apierr.Invalid("NT_EMAIL_REQUIRED", "请填写邮箱地址")
	}
	if !validSuppressionReason(reason) {
		reason = "MANUAL"
	}
	return s.q.AddSuppression(ctx, store.AddSuppressionParams{
		TenantID: tenantID, Email: email, Reason: reason, Detail: detail,
	})
}

func (s *Service) Unsuppress(ctx context.Context, tenantID int64, email string) error {
	n, err := s.q.RemoveSuppression(ctx, store.RemoveSuppressionParams{
		TenantID: tenantID, Email: strings.ToLower(strings.TrimSpace(email)),
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("NT_SUPPRESSION_NOT_FOUND", "这个地址不在名单里")
	}
	return nil
}

func validSuppressionReason(r string) bool {
	switch r {
	case "HARD_BOUNCE", "COMPLAINT", "UNSUBSCRIBE", "MANUAL":
		return true
	}
	return false
}
