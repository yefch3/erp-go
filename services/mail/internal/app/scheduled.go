package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// How far ahead a send may be booked. A year is past any real use — "next
// season's price list" is months, not years — and it catches the typed 2026
// that should have been 2025 before it becomes a mail nobody remembers
// scheduling.
const scheduleHorizon = 365 * 24 * time.Hour

// validSchedule turns a requested moment into what the queue stores.
//
// A time in the past is refused rather than quietly sent now: somebody who
// typed yesterday's date meant a different day, and delivering immediately
// would be the one outcome they did not ask for.
func validSchedule(at time.Time) (pgtype.Timestamptz, error) {
	if at.IsZero() {
		return pgtype.Timestamptz{}, nil
	}
	now := time.Now()
	if !at.After(now) {
		return pgtype.Timestamptz{}, apierr.Invalid("NT_SCHEDULE_PAST",
			"定时发送时间必须晚于当前时间")
	}
	if at.After(now.Add(scheduleHorizon)) {
		return pgtype.Timestamptz{}, apierr.Invalid("NT_SCHEDULE_TOO_FAR",
			"定时发送最多只能预约一年内的时间")
	}
	return pgtype.Timestamptz{Time: at, Valid: true}, nil
}

// ScheduledSend is one send waiting for its moment.
type ScheduledSend struct {
	CampaignID   int64
	CampaignNo   string
	Subject      string
	PendingCount int32
	ToNames      string
	ScheduledAt  time.Time
	SendMode     string
	BodyFormat   string
}

// ListScheduled reports what the caller has booked and not yet sent.
func (s *Service) ListScheduled(ctx context.Context, tenantID int64, op Operator, limit, offset int32) ([]ScheduledSend, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.q.ListScheduled(ctx, store.ListScheduledParams{
		TenantID: tenantID, SenderID: op.ID, RowLimit: limit, RowOffset: offset,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]ScheduledSend, 0, len(rows))
	var total int64
	for _, r := range rows {
		total = r.Total
		v := ScheduledSend{
			CampaignID: r.ID, CampaignNo: r.CampaignNo, Subject: r.SubjectTpl,
			PendingCount: r.PendingCount, ToNames: r.ToNames,
			SendMode: r.SendMode, BodyFormat: r.BodyFormat,
		}
		if r.ScheduledAt.Valid {
			v.ScheduledAt = r.ScheduledAt.Time
		}
		out = append(out, v)
	}
	return out, total, nil
}

// SendScheduledNow drops the hold so the next drain pass picks the send up.
func (s *Service) SendScheduledNow(ctx context.Context, tenantID, campaignID int64, op Operator) (int64, error) {
	if _, err := s.ownScheduled(ctx, tenantID, campaignID, op); err != nil {
		return 0, err
	}
	n, err := s.q.ReleaseScheduled(ctx, store.ReleaseScheduledParams{
		TenantID: tenantID, CampaignID: campaignID,
	})
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, apierr.Invalid("NT_SCHEDULE_GONE", "这封邮件已经发出，无需再次发送")
	}
	s.log.Info("scheduled send released", "campaign", campaignID, "messages", n)
	return n, nil
}

// CancelScheduled stops a send that has not gone out and puts it back in the
// drafts folder.
//
// Back to drafts rather than binned: calling a mail back almost always means
// changing something in it, and a cancel that destroys an hour's writing would
// make the button too frightening to use. The draft is written first — if that
// fails there is nothing to cancel yet, and the mail still goes as booked.
func (s *Service) CancelScheduled(ctx context.Context, tenantID, campaignID int64, op Operator) (int64, int64, error) {
	c, err := s.ownScheduled(ctx, tenantID, campaignID, op)
	if err != nil {
		return 0, 0, err
	}
	draft, err := s.restoreDraft(ctx, tenantID, c, op)
	if err != nil {
		return 0, 0, err
	}
	n, err := s.q.CancelScheduled(ctx, store.CancelScheduledParams{
		TenantID: tenantID, CampaignID: campaignID,
	})
	if err != nil {
		return 0, 0, err
	}
	if n == 0 {
		// The worker got there first. Take the draft back out rather than
		// leaving a copy of a mail that has already gone.
		if err := s.DeleteDraft(ctx, tenantID, draft, op); err != nil {
			s.log.Warn("could not clean up a draft for an uncancellable send",
				"campaign", campaignID, "draft", draft, "err", err)
		}
		return 0, 0, apierr.Invalid("NT_SCHEDULE_GONE", "这封邮件已经发出，来不及取消了")
	}
	s.log.Info("scheduled send cancelled", "campaign", campaignID,
		"messages", n, "draft", draft)
	return n, draft, nil
}

// ownScheduled resolves a campaign and proves it is the caller's to touch.
func (s *Service) ownScheduled(ctx context.Context, tenantID, campaignID int64, op Operator) (store.GetScheduledCampaignRow, error) {
	c, err := s.q.GetScheduledCampaign(ctx, store.GetScheduledCampaignParams{
		TenantID: tenantID, ID: campaignID,
	})
	if err == pgx.ErrNoRows {
		return c, apierr.NotFound("NT_CAMPAIGN_NOT_FOUND", "定时邮件不存在")
	}
	if err != nil {
		return c, err
	}
	// Not-found rather than forbidden, as everywhere else here: whether
	// somebody else has a send booked is not the caller's to learn.
	if c.SenderID != op.ID {
		return c, apierr.NotFound("NT_CAMPAIGN_NOT_FOUND", "定时邮件不存在")
	}
	return c, nil
}

// restoreDraft rebuilds the compose window a scheduled send came from.
//
// From the campaign's template rather than the rendered messages: the template
// still has its variables in it, which is what a person can edit again. The
// signature is already joined into that body, so the restored draft carries no
// signature id — reattaching one would sign the mail twice.
func (s *Service) restoreDraft(ctx context.Context, tenantID int64, c store.GetScheduledCampaignRow, op Operator) (int64, error) {
	msgs, err := s.q.ListScheduledRecipients(ctx, store.ListScheduledRecipientsParams{
		TenantID: tenantID, CampaignID: c.ID,
	})
	if err != nil {
		return 0, err
	}
	in := DraftInput{
		Subject: c.SubjectTpl, Body: c.BodyTpl, Format: c.BodyFormat,
		Kind: c.Kind, SendMode: "SEPARATE", ReplyToInboundID: c.ReplyToInboundID,
	}
	if len(msgs) > 0 && msgs[0].SendMode == "MERGED" {
		// One message, and the cast lives beside it. To and CC are separate
		// fields in the composer, so they are separated again here.
		in.SendMode = "MERGED"
		recips, err := s.q.ListMessageRecipients(ctx, store.ListMessageRecipientsParams{
			TenantID: tenantID, MessageID: msgs[0].ID,
		})
		if err != nil {
			return 0, err
		}
		for _, r := range recips {
			one := Recipient{Email: r.Email, Name: r.Name}
			if r.Kind == "CC" {
				in.CC = append(in.CC, one)
			} else {
				in.Recipients = append(in.Recipients, one)
			}
		}
	} else {
		for _, m := range msgs {
			in.Recipients = append(in.Recipients, Recipient{
				Email: m.ToEmail, Name: m.ToName,
				CustomerID: m.CustomerID, CustomerName: m.CustomerName,
				ContactID: m.ContactID,
			})
		}
	}

	// The files are already in storage and already recorded against the
	// campaign; the draft only needs to point at them again.
	atts, err := s.q.ListAttachments(ctx, store.ListAttachmentsParams{
		TenantID: tenantID, CampaignID: c.ID,
	})
	if err != nil {
		return 0, err
	}
	for _, a := range atts {
		in.Attachments = append(in.Attachments, PendingAttachment{
			FileName: a.FileName, FileKey: a.FileKey,
		})
	}
	return s.SaveDraft(ctx, tenantID, in, op)
}
