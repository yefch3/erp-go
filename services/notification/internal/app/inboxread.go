package app

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

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
	// How many messages the list row stands for. 1 for a lone message; the
	// list collapses a conversation into one row and shows this count.
	ThreadCount int32
}

// InboundPage is one screenful of conversations plus the counts and the
// cursor that reaches the next one.
type InboundPage struct {
	Mails  []InboundView
	Total  int64
	Unread int32
	// Empty when this is the last page. Opaque to the caller: it encodes the
	// sort position of the final row, not an offset.
	NextCursor string
}

// ListInbound is the caller's own inbox, one page at a time.
//
// ownerID is always the caller. There is deliberately no way to pass another
// person's id here: the supervisor view reads through its own scoped surface,
// and this one answers only "my mail".
//
// Paging is keyset: cursor names the last row of the previous page and the
// next one starts strictly after it. An empty cursor is the first page. Mail
// arriving while somebody reads therefore cannot shift the boundary and make
// a conversation show up twice or slip past unseen, which is exactly what
// OFFSET does on a list that grows at the top.
func (s *Service) ListInbound(ctx context.Context, tenantID, ownerID int64, keyword, view, cursor string, size int32) (InboundPage, error) {
	_, size = normalizePage(1, size)
	// An unknown view falls back to the inbox proper rather than erroring:
	// the worst a bad parameter can do is show the default slice.
	switch view {
	case "STARRED", "ARCHIVE", "TRASH", "JUNK":
	default:
		view = "INBOX"
	}

	at, id, err := decodeCursor(cursor)
	if err != nil {
		return InboundPage{}, err
	}

	// One row per conversation, not per message: the newest message speaks
	// for the thread and carries a count. Opening it shows the whole
	// exchange, which the reading page has done since the conversation view.
	rows, err := s.q.ListInboundThreads(ctx, store.ListInboundThreadsParams{
		TenantID: tenantID, OwnerID: ownerID, Keyword: keyword, View: view,
		CursorAt: at, CursorID: id, RowLimit: size,
	})
	if err != nil {
		return InboundPage{}, err
	}
	// Still counted: the person wants to know how much mail is in here, and
	// keyset paging only replaces how pages are reached, not what they show.
	total, err := s.q.CountInboundThreads(ctx, store.CountInboundThreadsParams{
		TenantID: tenantID, OwnerID: ownerID, Keyword: keyword, View: view,
	})
	if err != nil {
		return InboundPage{}, err
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
			ThreadCount: r.ThreadCount,
		}
		if r.ReceivedAt.Valid {
			v.ReceivedAt = r.ReceivedAt.Time
		}
		if r.SentAt.Valid {
			v.SentAt = r.SentAt.Time
		}
		out = append(out, v)
	}

	page := InboundPage{Mails: out, Total: total, Unread: int32(unread)}
	// A short page is the end of the list. A full one might be, and offering
	// a next page that turns out empty is a smaller sin than hiding mail.
	if int32(len(out)) == size && size > 0 {
		last := out[len(out)-1]
		page.NextCursor = encodeCursor(sortTime(last), last.ID)
	}
	return page, nil
}

// sortTime mirrors the query's coalesce(sent_at, received_at). The cursor has
// to name the same instant the ORDER BY used, or a page boundary would land
// in the wrong place.
func sortTime(v InboundView) time.Time {
	if !v.SentAt.IsZero() {
		return v.SentAt
	}
	return v.ReceivedAt
}

// The cursor is a position, not a page number: the sort key of the last row
// shown. Encoded so it reads as an opaque token — nothing downstream should
// be tempted to do arithmetic on it.
func encodeCursor(at time.Time, id int64) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(strconv.FormatInt(at.UTC().UnixMicro(), 10) + ":" + strconv.FormatInt(id, 10)))
}

func decodeCursor(cursor string) (pgtype.Timestamptz, int64, error) {
	if cursor == "" {
		return pgtype.Timestamptz{}, 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return pgtype.Timestamptz{}, 0, errBadCursor()
	}
	micros, idPart, ok := strings.Cut(string(raw), ":")
	if !ok {
		return pgtype.Timestamptz{}, 0, errBadCursor()
	}
	us, err := strconv.ParseInt(micros, 10, 64)
	if err != nil {
		return pgtype.Timestamptz{}, 0, errBadCursor()
	}
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil {
		return pgtype.Timestamptz{}, 0, errBadCursor()
	}
	return pgtype.Timestamptz{Time: time.UnixMicro(us).UTC(), Valid: true}, id, nil
}

func errBadCursor() error {
	return apierr.Invalid("NT_BAD_CURSOR", "翻页位置无效，请回到第一页")
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

// ThreadItem is one turn of a conversation, either direction.
type ThreadItem struct {
	Direction    string
	ID           int64
	Subject      string
	Body         string
	BodyFormat   string
	Counterparty string
	Who          string
	At           time.Time
}

// GetMailThread returns one conversation, oldest first, both directions.
//
// Inbound HTML is sanitised on the way out, same as GetInbound: these bodies
// came from the wild and are about to be rendered inside our page. Outbound
// bodies were sanitised when they were composed.
func (s *Service) GetMailThread(ctx context.Context, tenantID, ownerID int64, threadKey string) ([]ThreadItem, error) {
	if strings.TrimSpace(threadKey) == "" {
		return nil, apierr.Invalid("NT_THREAD_KEY_REQUIRED", "缺少会话标识")
	}
	rows, err := s.q.ListThread(ctx, store.ListThreadParams{
		TenantID: tenantID, OwnerID: ownerID, ThreadKey: threadKey,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ThreadItem, 0, len(rows))
	for _, r := range rows {
		body := r.Body
		if r.Direction == "IN" && r.BodyFormat == "HTML" {
			body = SanitizeHTML(body)
		}
		v := ThreadItem{
			Direction: r.Direction, ID: r.ID, Subject: r.Subject,
			Body: body, BodyFormat: r.BodyFormat,
			Counterparty: r.Counterparty, Who: r.Who,
		}
		if r.At.Valid {
			v.At = r.At.Time
		}
		out = append(out, v)
	}
	return out, nil
}

// MarkInbound is inbox housekeeping: read/unread, star, archive, trash.
//
// ERP-side state only, never written back to the mail host — the sync stays
// one-way, so no housekeeping bug can ever damage the real mailbox. A nil
// flag leaves that flag alone. Owner-scoped in the query: marking a mail
// that is not the caller's is a silent no-op, not information.
//
// wholeThread applies the change to every message of the conversation. The
// list shows one row per conversation, so archiving from there has to move
// the conversation; moving only its newest message would leave the row in
// place, one message lighter.
func (s *Service) MarkInbound(ctx context.Context, tenantID, ownerID, id int64, read, starred, archived, deleted, notJunk *bool, wholeThread bool) error {
	if wholeThread {
		row, err := s.q.GetInbound(ctx, store.GetInboundParams{TenantID: tenantID, ID: id})
		// A loose message has no thread key to spread across, and somebody
		// else's mail is not ours to look up — both fall through to marking
		// the single row, which is owner-scoped in its own right.
		if err == nil && row.OwnerID == ownerID && row.ThreadKey != "" {
			return s.q.SetThreadFlags(ctx, store.SetThreadFlagsParams{
				TenantID: tenantID, OwnerID: ownerID, ThreadKey: row.ThreadKey,
				Read: read, Starred: starred, Archived: archived, Deleted: deleted,
				NotJunk: notJunk,
			})
		}
	}
	return s.q.SetInboundFlags(ctx, store.SetInboundFlagsParams{
		TenantID: tenantID, OwnerID: ownerID, ID: id,
		Read: read, Starred: starred, Archived: archived, Deleted: deleted,
		NotJunk: notJunk,
	})
}

// PurgeInbound permanently deletes mail from the caller's trash: the database
// records and their copies in object storage (raw MIME, extracted
// attachments). Only ERP-side data — the mail host's original is untouched,
// because the sync is one-way and nothing here talks to the host at all.
//
// wholeThread deletes every trashed message of the conversation, matching
// what the trash list shows: one row per conversation. A live message of the
// same thread is never swept up — only what is already in the trash.
func (s *Service) PurgeInbound(ctx context.Context, tenantID, ownerID, id int64, wholeThread bool) error {
	row, err := s.q.GetInboundForPurge(ctx, store.GetInboundForPurgeParams{
		TenantID: tenantID, OwnerID: ownerID, ID: id,
	})
	if err != nil {
		// Covers "not yours" and "not in the trash" alike: neither is the
		// caller's to distinguish.
		return errNotFound()
	}

	type target struct {
		id     int64
		rawKey string
	}
	targets := []target{{id: row.ID, rawKey: row.RawKey}}
	if wholeThread {
		full, err := s.q.GetInbound(ctx, store.GetInboundParams{TenantID: tenantID, ID: id})
		if err == nil && full.OwnerID == ownerID && full.ThreadKey != "" {
			rows, err := s.q.ListThreadForPurge(ctx, store.ListThreadForPurgeParams{
				TenantID: tenantID, OwnerID: ownerID, ThreadKey: full.ThreadKey,
			})
			if err != nil {
				return err
			}
			targets = targets[:0]
			for _, r := range rows {
				targets = append(targets, target{id: r.ID, rawKey: r.RawKey})
			}
		}
	}

	for _, tg := range targets {
		if err := s.purgeOne(ctx, tenantID, ownerID, tg.id, tg.rawKey); err != nil {
			return err
		}
	}
	return nil
}

// purgeOne removes one message's objects and then its row.
//
// Objects first, row last. The row is the retry handle: if an object removal
// fails halfway the mail stays in the trash and a second attempt covers
// whatever remains (removals are idempotent). Deleting the row first would
// leave orphaned objects with nothing pointing at them.
func (s *Service) purgeOne(ctx context.Context, tenantID, ownerID, id int64, rawKey string) error {
	if s.files != nil {
		atts, err := s.q.ListInboundAttachments(ctx, store.ListInboundAttachmentsParams{
			TenantID: tenantID, InboundID: id,
		})
		if err != nil {
			return err
		}
		keys := make([]string, 0, len(atts)+1)
		for _, a := range atts {
			if a.FileKey != "" {
				keys = append(keys, a.FileKey)
			}
		}
		if rawKey != "" {
			keys = append(keys, rawKey)
		}
		for _, key := range keys {
			if err := s.files.Remove(ctx, key); err != nil {
				return apierr.Internal("NT_PURGE_STORAGE", "对象存储删除失败，请重试")
			}
		}
	}

	n, err := s.q.PurgeInbound(ctx, store.PurgeInboundParams{
		TenantID: tenantID, OwnerID: ownerID, ID: id,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotFound()
	}
	return nil
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
