package app

import (
	"context"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/notification/internal/store"
)

// InboundView is one received message as the API returns it.
type InboundView struct {
	ID             int64
	FromEmail      string
	FromName       string
	ToEmail        string
	Subject        string
	Snippet        string
	ThreadKey      string
	IsRead         bool
	IsStarred      bool
	HasAttachments bool
	ReceivedAt     time.Time
	SentAt         time.Time
	BodyHTML       string
	BodyText       string
	Attachments    []Attachment
}

// ListInbound is the caller's own inbox.
//
// ownerID is always the caller. There is deliberately no way to pass another
// person's id here: the supervisor view reads through its own scoped surface,
// and this one answers only "my mail".
func (s *Service) ListInbound(ctx context.Context, tenantID, ownerID int64, keyword, view string, page, size int32) ([]InboundView, int64, int32, error) {
	page, size = normalizePage(page, size)
	// An unknown view falls back to the inbox proper rather than erroring:
	// the worst a bad parameter can do is show the default slice.
	switch view {
	case "STARRED", "ARCHIVE", "TRASH", "JUNK":
	default:
		view = "INBOX"
	}
	rows, err := s.q.ListInbound(ctx, store.ListInboundParams{
		TenantID: tenantID, OwnerID: ownerID, Keyword: keyword, View: view,
		RowLimit: size, RowOffset: (page - 1) * size,
	})
	if err != nil {
		return nil, 0, 0, err
	}
	total, err := s.q.CountInbound(ctx, store.CountInboundParams{
		TenantID: tenantID, OwnerID: ownerID, Keyword: keyword, View: view,
	})
	if err != nil {
		return nil, 0, 0, err
	}
	unread, err := s.q.CountUnread(ctx, store.CountUnreadParams{
		TenantID: tenantID, OwnerID: ownerID,
	})
	if err != nil {
		unread = 0
	}

	out := make([]InboundView, 0, len(rows))
	for _, r := range rows {
		v := InboundView{
			ID: r.ID, FromEmail: r.FromEmail, FromName: r.FromName,
			Subject: r.Subject, Snippet: r.Snippet, ThreadKey: r.ThreadKey,
			IsRead: r.IsRead, IsStarred: r.IsStarred, HasAttachments: r.HasAttachments,
		}
		if r.ReceivedAt.Valid {
			v.ReceivedAt = r.ReceivedAt.Time
		}
		if r.SentAt.Valid {
			v.SentAt = r.SentAt.Time
		}
		out = append(out, v)
	}
	return out, total, int32(unread), nil
}

// GetInbound opens one message, marking it read as a side effect.
//
// Marking on open rather than by a separate call because that is what opening
// means: the person has seen it. This is our own inbox, so unlike the
// customer-side "opened" signal there is nothing probabilistic about it.
func (s *Service) GetInbound(ctx context.Context, tenantID, ownerID, id int64) (InboundView, error) {
	row, err := s.q.GetInbound(ctx, store.GetInboundParams{TenantID: tenantID, ID: id})
	if err != nil {
		return InboundView{}, errNotFound()
	}
	// Owner only. Not-found rather than forbidden: whether a given mail
	// exists in somebody else's inbox is itself not the caller's to learn.
	if row.OwnerID != ownerID {
		return InboundView{}, errNotFound()
	}

	if !row.IsRead {
		if err := s.q.MarkInboundRead(ctx, store.MarkInboundReadParams{
			TenantID: tenantID, OwnerID: ownerID, ID: id,
		}); err != nil {
			s.log.Warn("could not mark a mail read", "id", id, "err", err)
		}
	}

	v := InboundView{
		ID: row.ID, FromEmail: row.FromEmail, FromName: row.FromName,
		ToEmail: row.ToEmail, Subject: row.Subject, ThreadKey: row.ThreadKey,
		IsRead: true, HasAttachments: row.HasAttachments,
		// Sanitised on the way out, not just on the way in: this HTML came
		// from the wild, and it is about to be rendered inside our page.
		BodyHTML: SanitizeHTML(row.BodyHtml),
		BodyText: row.BodyText,
	}
	if row.ReceivedAt.Valid {
		v.ReceivedAt = row.ReceivedAt.Time
	}
	if row.SentAt.Valid {
		v.SentAt = row.SentAt.Time
	}

	atts, err := s.q.ListInboundAttachments(ctx, store.ListInboundAttachmentsParams{
		TenantID: tenantID, InboundID: id,
	})
	if err == nil {
		for _, a := range atts {
			v.Attachments = append(v.Attachments, Attachment{
				ID: a.ID, FileName: a.FileName, ContentType: a.ContentType,
				FileSize: a.FileSize, FileKey: a.FileKey,
			})
		}
	}
	return v, nil
}

// MarkInbound is inbox housekeeping: read/unread, star, archive, trash.
//
// ERP-side state only, never written back to the mail host — the sync stays
// one-way, so no housekeeping bug can ever damage the real mailbox. A nil
// flag leaves that flag alone. Owner-scoped in the query: marking a mail
// that is not the caller's is a silent no-op, not information.
func (s *Service) MarkInbound(ctx context.Context, tenantID, ownerID, id int64, read, starred, archived, deleted *bool) error {
	return s.q.SetInboundFlags(ctx, store.SetInboundFlagsParams{
		TenantID: tenantID, OwnerID: ownerID, ID: id,
		Read: read, Starred: starred, Archived: archived, Deleted: deleted,
	})
}

// SyncNow pulls the caller's mailbox immediately and reports what arrived.
func (s *Service) SyncNow(ctx context.Context, tenantID, employeeID int64) (int, error) {
	if s.mailbox == nil {
		return 0, ErrMailHostNotConfigured
	}
	return s.SyncMailbox(ctx, SyncConfig{TenantID: tenantID}, employeeID)
}

func errNotFound() error {
	return apierr.NotFound("NT_MESSAGE_NOT_FOUND", "邮件记录不存在")
}

// ListMailboxSent is the caller's own sent history, as the mail host holds it.
func (s *Service) ListMailboxSent(ctx context.Context, tenantID, ownerID int64, keyword string, page, size int32) ([]InboundView, int64, error) {
	page, size = normalizePage(page, size)
	rows, err := s.q.ListMailboxSent(ctx, store.ListMailboxSentParams{
		TenantID: tenantID, OwnerID: ownerID, Keyword: keyword,
		RowLimit: size, RowOffset: (page - 1) * size,
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.q.CountMailboxSent(ctx, store.CountMailboxSentParams{
		TenantID: tenantID, OwnerID: ownerID, Keyword: keyword,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]InboundView, 0, len(rows))
	for _, r := range rows {
		v := InboundView{
			ID: r.ID, FromEmail: r.FromEmail, FromName: r.FromName,
			ToEmail: r.ToEmail, Subject: r.Subject, Snippet: r.Snippet,
			ThreadKey: r.ThreadKey, IsRead: true, HasAttachments: r.HasAttachments,
		}
		if r.ReceivedAt.Valid {
			v.ReceivedAt = r.ReceivedAt.Time
		}
		if r.SentAt.Valid {
			v.SentAt = r.SentAt.Time
		}
		out = append(out, v)
	}
	return out, total, nil
}
