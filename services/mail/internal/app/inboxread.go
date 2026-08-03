package app

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
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

// sortTime mirrors the query's ORDER BY, which is received_at — when the mail
// host says it arrived. The cursor has to name the same instant the ordering
// used, or a page boundary lands in the wrong place, so these two move
// together or not at all.
func sortTime(v InboundView) time.Time {
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
		touched, err := s.q.MarkInboundRead(ctx, store.MarkInboundReadParams{
			TenantID: tenantID, OwnerID: ownerID, ID: id,
		})
		if err != nil {
			s.log.Warn("could not mark a mail read", "id", id, "err", err)
		}
		// Opening a mail here marks it read in the real mailbox too, which is
		// what anybody who also uses Gmail expects: they read it once.
		for _, t := range touched {
			s.queueFlagWrite(ctx, tenantID, t.AccountID, ownerID, t.Folder, t.ImapUid, flagSeen, true)
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
		// Signed here rather than at ingest: a URL minted when the mail
		// arrived would have expired long before anybody opened it.
		v.Attachments = s.signDownloads(ctx, v.Attachments)
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
// Applied here first and queued for the host second: what the person sees has
// to change the moment they click, and the mailbox catches up within seconds.
// A nil flag leaves that flag alone. Owner-scoped in the query: marking a mail
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
			touched, err := s.q.SetThreadFlags(ctx, store.SetThreadFlagsParams{
				TenantID: tenantID, OwnerID: ownerID, ThreadKey: row.ThreadKey,
				Read: read, Starred: starred, Archived: archived, Deleted: deleted,
				NotJunk: notJunk,
			})
			if err != nil {
				return err
			}
			rows := touchedThread(touched)
			publishFlags(ctx, s, tenantID, ownerID, rows, read != nil, starred != nil)
			publishMoves(ctx, s, tenantID, ownerID, rows, archived != nil, deleted != nil, notJunk != nil)
			return nil
		}
	}
	touched, err := s.q.SetInboundFlags(ctx, store.SetInboundFlagsParams{
		TenantID: tenantID, OwnerID: ownerID, ID: id,
		Read: read, Starred: starred, Archived: archived, Deleted: deleted,
		NotJunk: notJunk,
	})
	if err != nil {
		return err
	}
	// All four go up to the host now. Read and star are flags; archive and
	// trash are moves between folders, and a host without an archive folder
	// simply keeps its mail where it is — see publishMove.
	rows := touchedSingle(touched)
	publishFlags(ctx, s, tenantID, ownerID, rows, read != nil, starred != nil)
	publishMoves(ctx, s, tenantID, ownerID, rows, archived != nil, deleted != nil, notJunk != nil)
	return nil
}

// touchedRow is what a housekeeping update reports back about one message.
type touchedRow struct {
	accountID int64
	folder    string
	uid       int64
	messageID string
	read      bool
	starred   bool
	archived  bool
	deleted   bool
	notJunk   bool
}

func touchedSingle(rows []store.SetInboundFlagsRow) []touchedRow {
	out := make([]touchedRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, touchedRow{
			accountID: r.AccountID, folder: r.Folder, uid: r.ImapUid,
			messageID: r.MessageID, read: r.IsRead, starred: r.IsStarred,
			archived: r.ArchivedAt.Valid, deleted: r.DeletedAt.Valid,
			notJunk: r.NotJunk,
		})
	}
	return out
}

func touchedThread(rows []store.SetThreadFlagsRow) []touchedRow {
	out := make([]touchedRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, touchedRow{
			accountID: r.AccountID, folder: r.Folder, uid: r.ImapUid,
			messageID: r.MessageID, read: r.IsRead, starred: r.IsStarred,
			archived: r.ArchivedAt.Valid, deleted: r.DeletedAt.Valid,
			notJunk: r.NotJunk,
		})
	}
	return out
}

// publishFlags queues only the flags this call actually set. Queueing the
// others would publish their current value as though it were a fresh
// decision, and on a mail somebody had just changed in Gmail that would undo
// them.
func publishFlags(ctx context.Context, s *Service, tenantID, ownerID int64, rows []touchedRow, didRead, didStar bool) {
	for _, r := range rows {
		if didRead {
			s.queueFlagWrite(ctx, tenantID, r.accountID, ownerID, r.folder, r.uid, flagSeen, r.read)
		}
		if didStar {
			s.queueFlagWrite(ctx, tenantID, r.accountID, ownerID, r.folder, r.uid, flagFlagged, r.starred)
		}
	}
}

// publishMoves carries deletion, archiving and junk rescue up to the host.
// Separate from the flags because these move the message rather than label
// it, and because only the ones this call actually decided should travel.
func publishMoves(ctx context.Context, s *Service, tenantID, ownerID int64, rows []touchedRow, didArchive, didDelete, didNotJunk bool) {
	for _, r := range rows {
		if didDelete {
			s.queueFolderMove(ctx, tenantID, r.accountID, ownerID, r.folder, r.uid, r.messageID, flagTrash, r.deleted)
		}
		if didArchive {
			s.queueFolderMove(ctx, tenantID, r.accountID, ownerID, r.folder, r.uid, r.messageID, flagArchive, r.archived)
		}
		// Only in one direction. "This is not spam" moves the mail back to the
		// inbox and tells the provider's filter it misjudged the sender;
		// undoing that would mean asking the host to call it spam again, which
		// is not something this screen offers or should.
		if didNotJunk && r.notJunk && r.folder == "JUNK" {
			s.queueFolderMove(ctx, tenantID, r.accountID, ownerID, r.folder, r.uid, r.messageID, flagNotJunk, true)
		}
	}
}

// MarkViewRead marks everything in one view read and reports how many rows
// changed.
//
// Deliberately scoped to the view rather than the whole mailbox: the button
// sits above a list, and it should do what the list shows. Marking the junk
// view read must not silently clear the inbox.
func (s *Service) MarkViewRead(ctx context.Context, tenantID, ownerID int64, view string) (int64, error) {
	switch view {
	case "STARRED", "ARCHIVE", "TRASH", "JUNK":
	default:
		view = "INBOX"
	}
	touched, err := s.q.MarkViewRead(ctx, store.MarkViewReadParams{
		TenantID: tenantID, OwnerID: ownerID, View: view,
	})
	if err != nil {
		return 0, err
	}
	for _, t := range touched {
		s.queueFlagWrite(ctx, tenantID, t.AccountID, ownerID, t.Folder, t.ImapUid, flagSeen, true)
	}
	return int64(len(touched)), nil
}

// PurgeInbound permanently deletes mail from the caller's trash: the database
// records and their copies in object storage (raw MIME, extracted
// attachments), and the host's copy with them: "彻底删除" that leaves the mail
// sitting in Gmail would not be one. The host deletion is queued before the
// row goes, because the row is where the Message-ID that finds it lives.
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
		id        int64
		rawKey    string
		accountID int64
		folder    string
		uid       int64
		messageID string
	}
	targets := []target{{
		id: row.ID, rawKey: row.RawKey, accountID: row.AccountID,
		folder: row.Folder, uid: row.ImapUid, messageID: row.MessageID,
	}}
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
				targets = append(targets, target{
					id: r.ID, rawKey: r.RawKey, accountID: r.AccountID,
					folder: r.Folder, uid: r.ImapUid, messageID: r.MessageID,
				})
			}
		}
	}

	for _, tg := range targets {
		// Queued before the row goes: the queue row needs the Message-ID, and
		// after the delete there is nowhere left to read it from. Ordered this
		// way, the worst case is an op for a mail the ERP has already
		// forgotten — which still deletes the right message on the host.
		s.queueFolderMove(ctx, tenantID, tg.accountID, ownerID, tg.folder, tg.uid, tg.messageID, flagPurge, true)
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

// Trash keeps itself. Thirty days matches what Gmail and Outlook do, and the
// number matters less than the promise: deleted mail is recoverable for a
// while and then it is not, without anybody having to remember to tidy up.
const (
	trashRetention    = 30 * 24 * time.Hour
	trashSweepEvery   = 6 * time.Hour
	trashSweepPerPass = 200
)

// EmptyJunk moves everything in the junk view to the trash.
//
// Deliberately the trash and not oblivion. Gmail's "delete all spam now" is
// permanent, but the mail worth finding in a spam folder is the customer
// enquiry the filter got wrong, and that mistake is usually noticed a day
// later. Thirty days in the trash is the difference between losing the order
// and recovering it; anybody who really wants it gone empties the trash.
//
// Mails already rescued with 「这不是垃圾」 are left alone: they stopped being
// junk the moment somebody said so.
func (s *Service) EmptyJunk(ctx context.Context, tenantID, ownerID int64) (int, error) {
	rows, err := s.q.TrashJunkView(ctx, store.TrashJunkViewParams{
		TenantID: tenantID, OwnerID: ownerID,
	})
	if err != nil {
		return 0, err
	}
	// Carried to the host as well, exactly like deleting one by hand — a spam
	// folder emptied here and still full in Gmail would be a chore done twice.
	for _, r := range rows {
		s.queueFolderMove(ctx, tenantID, r.AccountID, ownerID, r.Folder, r.ImapUid, r.MessageID, flagTrash, true)
	}
	if len(rows) > 0 {
		s.log.Info("junk emptied into the trash", "owner", ownerID, "count", len(rows))
	}
	return len(rows), nil
}

// EmptyTrash purges everything in one person's trash.
//
// The same permanent deletion as one mail at a time, applied to the lot: ERP
// row, stored MIME and attachments, and the copy on the mail host. Reports
// how many went so the caller can say something truthful afterwards.
func (s *Service) EmptyTrash(ctx context.Context, tenantID, ownerID int64) (int, error) {
	rows, err := s.q.ListTrashForPurge(ctx, store.ListTrashForPurgeParams{
		TenantID: tenantID, OwnerID: ownerID,
	})
	if err != nil {
		return 0, err
	}
	done := 0
	for _, r := range rows {
		s.queueFolderMove(ctx, tenantID, r.AccountID, ownerID, r.Folder, r.ImapUid, r.MessageID, flagPurge, true)
		if err := s.purgeOne(ctx, tenantID, ownerID, r.ID, r.RawKey); err != nil {
			// One stubborn object must not strand the rest of the trash. The
			// row stays, so the next attempt picks it up again.
			s.log.Warn("could not purge one mail while emptying the trash",
				"id", r.ID, "err", err)
			continue
		}
		done++
	}
	return done, nil
}

// RunTrashSweeper clears out trash that has sat long enough.
//
// Runs on a slow tick — this is housekeeping, not a deadline. Each pass is
// bounded so a mailbox with years of deleted mail cannot monopolise the
// service on the first run after an upgrade.
func (s *Service) RunTrashSweeper(ctx context.Context, cfg SyncConfig) {
	cfg = cfg.withDefaults()
	s.log.Info("trash sweeper started", "keep", trashRetention, "every", trashSweepEvery)

	t := time.NewTicker(trashSweepEvery)
	defer t.Stop()
	for {
		s.sweepTrashOnce(ctx, cfg.TenantID)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) sweepTrashOnce(ctx context.Context, tenantID int64) {
	cutoff := time.Now().Add(-trashRetention)
	rows, err := s.q.ListExpiredTrash(ctx, store.ListExpiredTrashParams{
		TenantID: tenantID,
		Cutoff:   pgtype.Timestamptz{Time: cutoff, Valid: true},
		RowLimit: trashSweepPerPass,
	})
	if err != nil {
		s.log.Error("could not list expired trash", "err", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	done := 0
	for _, r := range rows {
		s.queueFolderMove(ctx, tenantID, r.AccountID, r.OwnerID, r.Folder, r.ImapUid, r.MessageID, flagPurge, true)
		if err := s.purgeOne(ctx, tenantID, r.OwnerID, r.ID, r.RawKey); err != nil {
			s.log.Warn("could not sweep one trashed mail", "id", r.ID, "err", err)
			continue
		}
		done++
	}
	s.log.Info("trash swept", "deleted", done, "older_than", trashRetention)
}
