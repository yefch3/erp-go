package app

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// DraftInput is a compose window saved mid-thought.
//
// Everything here is optional, including the subject and body. A draft with
// nothing in it but three recipients is a perfectly ordinary thing to save —
// validation belongs at send time, not here. Refusing to save an unfinished
// mail because it is unfinished would defeat the point.
type DraftInput struct {
	ID          int64
	Subject     string
	Body        string
	Format      string
	SignatureID int64
	Kind        string
	Recipients  []Recipient
	Attachments []PendingAttachment
}

// DraftView is one saved draft, restored into the composer.
type DraftView struct {
	ID          int64
	Subject     string
	Body        string
	Format      string
	SignatureID int64
	Kind        string
	Recipients  []Recipient
	Attachments []PendingAttachment
	UpdatedAt   string
}

// SaveDraft creates or updates. Both directions are the same call because the
// composer autosaves: the first save mints an id, every later one reuses it.
//
// The body is sanitised here too. A draft is reopened into an editor and can
// be previewed, so treating it as trusted just because it has not been sent
// would leave the same hole one step earlier.
func (s *Service) SaveDraft(ctx context.Context, tenantID int64, in DraftInput, op Operator) (int64, error) {
	format := normalizeFormat(in.Format)
	body := in.Body
	if format == FormatHTML {
		body = SanitizeHTML(body)
	}
	kind := in.Kind
	if kind != "TRANSACTIONAL" {
		kind = "MARKETING"
	}
	recipients, err := json.Marshal(orEmpty(in.Recipients))
	if err != nil {
		return 0, err
	}
	files, err := json.Marshal(orEmptyFiles(in.Attachments))
	if err != nil {
		return 0, err
	}
	id, err := s.q.SaveDraft(ctx, store.SaveDraftParams{
		ID: in.ID, TenantID: tenantID, OwnerID: op.ID,
		Subject: in.Subject, Body: body, BodyFormat: format,
		SignatureID: in.SignatureID, Kind: kind,
		Recipients: recipients, Attachments: files,
	})
	if err == pgx.ErrNoRows {
		// The upsert's WHERE refused it: the id exists but belongs to
		// somebody else. Not found rather than forbidden — whether another
		// person's draft exists is not something to confirm.
		return 0, apierr.NotFound("NT_DRAFT_NOT_FOUND", "草稿不存在")
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Service) ListDrafts(ctx context.Context, tenantID int64, op Operator) ([]store.ListDraftsRow, error) {
	return s.q.ListDrafts(ctx, store.ListDraftsParams{TenantID: tenantID, OwnerID: op.ID})
}

func (s *Service) GetDraft(ctx context.Context, tenantID, id int64, op Operator) (DraftView, error) {
	d, err := s.q.GetDraft(ctx, store.GetDraftParams{
		TenantID: tenantID, OwnerID: op.ID, ID: id,
	})
	if err == pgx.ErrNoRows {
		return DraftView{}, apierr.NotFound("NT_DRAFT_NOT_FOUND", "草稿不存在")
	}
	if err != nil {
		return DraftView{}, err
	}
	out := DraftView{
		ID: d.ID, Subject: d.Subject, Body: d.Body, Format: d.BodyFormat,
		SignatureID: d.SignatureID, Kind: d.Kind,
	}
	if d.UpdatedAt.Valid {
		out.UpdatedAt = d.UpdatedAt.Time.Format("2006-01-02T15:04:05Z07:00")
	}
	// Stored as JSONB and decoded back. A malformed blob would mean somebody
	// wrote to the table by hand; losing the recipients is better than
	// failing to open the draft at all.
	_ = json.Unmarshal(d.Recipients, &out.Recipients)
	_ = json.Unmarshal(d.Attachments, &out.Attachments)
	return out, nil
}

func (s *Service) DeleteDraft(ctx context.Context, tenantID, id int64, op Operator) error {
	n, err := s.q.DeleteDraft(ctx, store.DeleteDraftParams{
		TenantID: tenantID, OwnerID: op.ID, ID: id,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("NT_DRAFT_NOT_FOUND", "草稿不存在")
	}
	return nil
}

// SendDraft promotes a draft into a real send, then removes it.
//
// The send runs the ordinary create path — that is what draws the number,
// writes one message row per recipient and registers the attachments. The
// draft is only deleted afterwards: if the send fails validation the draft
// is still there, which is the difference between a rejected send and a lost
// morning's work.
func (s *Service) SendDraft(ctx context.Context, tenantID, id int64, op Operator) (CampaignResult, error) {
	d, err := s.GetDraft(ctx, tenantID, id, op)
	if err != nil {
		return CampaignResult{}, err
	}
	res, err := s.CreateCampaign(ctx, tenantID, CampaignInput{
		Subject: d.Subject, Body: d.Body, Format: d.Format,
		SignatureID: d.SignatureID, Kind: d.Kind,
		Recipients: d.Recipients, Attachments: d.Attachments,
	}, op)
	if err != nil {
		return CampaignResult{}, err
	}
	if err := s.DeleteDraft(ctx, tenantID, id, op); err != nil {
		// The mail is already queued; failing here would tell the caller the
		// send did not happen when it did. A stale draft is the lesser harm.
		s.log.Error("draft sent but not cleaned up", "draft_id", id, "err", err)
	}
	return res, nil
}

func orEmpty(in []Recipient) []Recipient {
	if in == nil {
		return []Recipient{}
	}
	return in
}

func orEmptyFiles(in []PendingAttachment) []PendingAttachment {
	if in == nil {
		return []PendingAttachment{}
	}
	return in
}
