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
	// The conversation quoted back inside this message, split out so the
	// reader can fold it. Empty means there was nothing worth folding, which
	// is the answer for most mail — see SplitQuotedHistory.
	QuotedHTML  string
	Attachments []Attachment
	// How many messages the list row stands for. 1 for a lone message; the
	// list collapses a conversation into one row and shows this count.
	ThreadCount int32

	// Sent folder only. "HOST" is a real message in the host's Sent folder —
	// every mailbox action works on it. "ERP" is a delivery record the host
	// never kept a copy of, shown after a grace period so a mail that was
	// genuinely sent is never missing; it has no message to act on.
	Kind     string
	ToName   string
	Status   string
	OpenedAt time.Time
	// Whether a tracking pixel actually went into this send. The third state,
	// and it cannot be inferred: "nobody opened it" and "nobody was watching"
	// are the same empty OpenedAt, and only the first says anything about the
	// recipient.
	Tracked bool

	// Which mailbox folder this copy sits in. SENT is what tells the reader to
	// show 对方是否已读; once both are an InboundView, nothing else does.
	Folder string
	// The RFC 5322 Message-ID and the size of the stored MIME — the details
	// panel, not the reader.
	MessageIDHeader string
	RawSize         int64

	// Whether the original MIME is still in object storage, which is what
	// forward-as-attachment sends. The send path refuses without it anyway;
	// this is so the screen can grey the action out instead of letting
	// somebody write a whole mail and then be told it cannot go.
	HasRaw bool
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

// threadRow is one conversation as the list needs it, from whichever of the
// two queries produced it. sqlc generates a row type per query and the two are
// structurally identical, so this is the seam that keeps the mapping below
// from being written twice and drifting.
type threadRow struct {
	ID                          int64
	FromEmail, FromName         string
	Subject, Snippet, ThreadKey string
	IsRead, IsStarred           bool
	HasAttachments              bool
	ThreadCount                 int32
	ReceivedAt, SentAt          pgtype.Timestamptz
}

func threadRowsFromView(rows []store.ListThreadsByViewRow) []threadRow {
	out := make([]threadRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, threadRow{
			ID: r.ID, FromEmail: r.FromEmail, FromName: r.FromName,
			Subject: r.Subject, Snippet: r.Snippet, ThreadKey: r.ThreadKey,
			IsRead: r.IsRead, IsStarred: r.IsStarred, HasAttachments: r.HasAttachments,
			ThreadCount: r.ThreadCount, ReceivedAt: r.ReceivedAt, SentAt: r.SentAt,
		})
	}
	return out
}

func threadRowsFromSearch(rows []store.ListInboundThreadsRow) []threadRow {
	out := make([]threadRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, threadRow{
			ID: r.ID, FromEmail: r.FromEmail, FromName: r.FromName,
			Subject: r.Subject, Snippet: r.Snippet, ThreadKey: r.ThreadKey,
			IsRead: r.IsRead, IsStarred: r.IsStarred, HasAttachments: r.HasAttachments,
			ThreadCount: r.ThreadCount, ReceivedAt: r.ReceivedAt, SentAt: r.SentAt,
		})
	}
	return out
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
	//
	// Two paths, and the split is about what can be precomputed. Without a
	// keyword the answer is a fact about the mailbox, and mail_thread_view
	// holds it already - the page reads the twenty-five rows it shows. With
	// one, the set of conversations depends on the search term, so it has to
	// be worked out per request, which is what the older query does. Search
	// is rare and its own screen; the plain list is loaded all day.
	var rows []threadRow
	var total int64
	if keyword == "" {
		fast, err := s.q.ListThreadsByView(ctx, store.ListThreadsByViewParams{
			TenantID: tenantID, OwnerID: ownerID, View: view,
			CursorAt: at, CursorID: id, RowLimit: size,
		})
		if err != nil {
			return InboundPage{}, err
		}
		rows = threadRowsFromView(fast)
		if total, err = s.q.CountThreadsByView(ctx, store.CountThreadsByViewParams{
			TenantID: tenantID, OwnerID: ownerID, View: view,
		}); err != nil {
			return InboundPage{}, err
		}
	} else {
		slow, err := s.q.ListInboundThreads(ctx, store.ListInboundThreadsParams{
			TenantID: tenantID, OwnerID: ownerID, Keyword: keyword, View: view,
			CursorAt: at, CursorID: id, RowLimit: size,
		})
		if err != nil {
			return InboundPage{}, err
		}
		rows = threadRowsFromSearch(slow)
		// Still counted: the person wants to know how much mail is in here,
		// and keyset paging only replaces how pages are reached, not what
		// they show.
		if total, err = s.q.CountInboundThreads(ctx, store.CountInboundThreadsParams{
			TenantID: tenantID, OwnerID: ownerID, Keyword: keyword, View: view,
		}); err != nil {
			return InboundPage{}, err
		}
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
		HasRaw: row.RawKey != "",
		Folder: row.Folder, MessageIDHeader: row.MessageID, RawSize: row.RawSize,
		// Null for anything the inbox reads: an inbound mail has no delivery
		// record, and coalesce already turned "no row" into the empty answer.
		Status: row.SentStatus, Tracked: row.SentTracked,
		// Sanitised on the way out, not just on the way in: this HTML came
		// from the wild, and it is about to be rendered inside our page.
		// Read with the wider reader policy: this goes into a sandboxed
		// frame, where a sender's stylesheet cannot reach our page.
		//
		// Then the pictures are swapped for our own copies, so that opening
		// this mail does not tell the sender it was opened. After sanitising,
		// deliberately: the substitution matches the addresses as the reader
		// will see them, and a signed storage URL never has to survive the
		// sanitiser's own URL policy.
		BodyText: row.BodyText,
	}
	// Sanitise, then localise the pictures, then fold — in that order. The
	// fold parses the markup, so it has to run on the form the reader will
	// actually be given; folding first and sanitising afterwards would mean
	// the sanitiser could move the boundary out from under the split.
	//
	// The sanitised body is kept as its own value because the attachment list
	// below has to be filtered against it. After the swap there is no cid:
	// left in the markup to test — it has all become signed storage URLs —
	// so asking the finished body which parts it embedded would always answer
	// "none", and every signature logo would go back to being listed.
	// Two kinds of picture, and they have to be swapped on opposite sides of
	// the sanitiser.
	//
	// Embedded ones go first, before sanitising. The reader policy allows http
	// and https and nothing else, so it does not merely reject a cid: src — it
	// strips the attribute, leaving an <img> with no source at all. That is
	// what the broken glyph in a signature really was: not a src the browser
	// could not follow, but no src whatsoever. Swapping first means the
	// sanitiser sees an ordinary https storage URL and vets it like any other.
	//
	// Remote ones go after, because their keys were recorded in the form the
	// sanitiser produces — it percent-encodes spaces in URLs on the way
	// through, and matching the raw form would miss those.
	embedded := s.embeddedSwap(ctx, tenantID, id)
	sanitised := SanitizeForReading(s.localiseImages(ctx, row.BodyHtml, embedded))
	v.BodyHTML, v.QuotedHTML = SplitQuotedHistory(
		s.localiseImages(ctx, sanitised, s.swapForMessage(ctx, tenantID, id)))
	if row.ReceivedAt.Valid {
		v.ReceivedAt = row.ReceivedAt.Time
	}
	if row.SentAt.Valid {
		v.SentAt = row.SentAt.Time
	}
	if row.SentOpenedAt.Valid {
		v.OpenedAt = row.SentOpenedAt.Time
	}

	atts, err := s.q.ListInboundAttachments(ctx, store.ListInboundAttachmentsParams{
		TenantID: tenantID, InboundID: id,
	})
	if err == nil {
		for _, a := range atts {
			v.Attachments = append(v.Attachments, Attachment{
				ID: a.ID, FileName: a.FileName, ContentType: a.ContentType,
				FileSize: a.FileSize, FileKey: a.FileKey, ContentID: a.ContentID,
			})
		}
		// Parts the body has already shown inline are not attachments to a
		// reader, whatever they are to the MIME structure. Tested against the
		// stored body, which is where the cid: references still live, and
		// against the swap, so a part we failed to resolve stays in the list
		// rather than vanishing from both the body and the attachments.
		v.Attachments = hideEmbedded(v.Attachments, row.BodyHtml, embedded)
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
	Quoted       string
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
	// One query for the whole conversation's cached pictures, and one
	// signature per distinct object. A sixteen-turn thread quoting the same
	// signature logo throughout would otherwise be sixteen round trips.
	swaps := s.swapForThread(ctx, tenantID, ownerID, threadKey)
	embedded := s.embeddedSwapForThread(ctx, tenantID, ownerID, threadKey)

	out := make([]ThreadItem, 0, len(rows))
	for _, r := range rows {
		body, quoted := r.Body, ""
		if r.Direction == "IN" && r.BodyFormat == "HTML" {
			// The fold matters most here and for the reason this view exists:
			// turn sixteen of a conversation is turns one to fifteen stacked
			// up, and the thread already shows those separately.
			// Embedded before the sanitiser, remote after — see GetInbound.
			body, quoted = SplitQuotedHistory(
				s.localiseImages(ctx,
					SanitizeForReading(s.localiseImages(ctx, body, embedded[r.ID])),
					swaps[r.ID]))
		}
		v := ThreadItem{
			Direction: r.Direction, ID: r.ID, Subject: r.Subject,
			Body: body, Quoted: quoted, BodyFormat: r.BodyFormat,
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
		// The pictures we fetched on this mail's behalf. The table row goes
		// with the message through the cascade; the bytes only go if they are
		// named here first.
		keys = append(keys, s.imageKeysFor(ctx, tenantID, id)...)
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
//
// Interactive: it answers once the inbox is in, and the sent folder, the junk
// folder and the read-state reconciliation carry on behind it. Somebody who
// clicks 立即收信 is asking whether the customer has replied, and waiting out
// five further round trips to be told so is the button feeling broken.
func (s *Service) SyncNow(ctx context.Context, tenantID, employeeID int64) (int, bool, error) {
	if s.mailbox == nil {
		return 0, false, ErrMailHostNotConfigured
	}
	return s.SyncMailboxInteractive(ctx, SyncConfig{TenantID: tenantID}, employeeID)
}

func errNotFound() error {
	return apierr.NotFound("NT_MESSAGE_NOT_FOUND", "邮件记录不存在")
}

// ListMailboxSent is the Sent folder, and it is a folder: every row is a real
// message the host is holding, so starring, archiving and deleting work here
// the same way they do in the inbox. See ListSentUnified for why the ERP's own
// delivery record is joined on rather than listed.
//
// Keyset like every other mailbox list. The cursor carries the kind as well as
// the time and id, because the two halves of the query number their rows in
// different tables and (at, id) alone is not a unique position.
func (s *Service) ListMailboxSent(ctx context.Context, tenantID, ownerID int64, keyword, cursor string, size int32) (InboundPage, error) {
	_, size = normalizePage(1, size)
	at, kind, id, err := decodeSentCursor(cursor)
	if err != nil {
		return InboundPage{}, err
	}
	rows, err := s.q.ListSentUnified(ctx, store.ListSentUnifiedParams{
		TenantID: tenantID, OwnerID: ownerID, Keyword: keyword,
		CursorAt: at, CursorKind: kind, CursorID: id, RowLimit: size,
	})
	if err != nil {
		return InboundPage{}, err
	}
	total, err := s.q.CountSentUnified(ctx, store.CountSentUnifiedParams{
		TenantID: tenantID, OwnerID: ownerID, Keyword: keyword,
	})
	if err != nil {
		return InboundPage{}, err
	}
	out := make([]InboundView, 0, len(rows))
	for _, r := range rows {
		v := InboundView{
			ID: r.ID, Kind: r.Kind, ToEmail: r.ToEmail, ToName: r.ToName,
			Subject: r.Subject, Snippet: r.Snippet, Status: r.Status,
			ThreadKey: r.ThreadKey, IsStarred: r.IsStarred,
			// Sent mail is mail you wrote; there is nothing to have not read.
			IsRead: true, HasAttachments: r.HasAttachments,
		}
		// One timestamp in both fields: the list sorts and displays on "when it
		// went out", and the two halves name that differently.
		if r.At.Valid {
			v.SentAt = r.At.Time
			v.ReceivedAt = r.At.Time
		}
		if r.OpenedAt.Valid {
			v.OpenedAt = r.OpenedAt.Time
		}
		out = append(out, v)
	}
	page := InboundPage{Mails: out, Total: total}
	if int32(len(out)) == size && size > 0 {
		last := out[len(out)-1]
		page.NextCursor = encodeSentCursor(last.SentAt, last.Kind, last.ID)
	}
	return page, nil
}

// The Sent cursor is (time, kind, id) — the sort key of the last row shown.
// Opaque on purpose: nothing downstream should do arithmetic on it.
func encodeSentCursor(at time.Time, kind string, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(
		strconv.FormatInt(at.UTC().UnixMicro(), 10) + ":" + kind + ":" + strconv.FormatInt(id, 10)))
}

func decodeSentCursor(cursor string) (pgtype.Timestamptz, string, int64, error) {
	if cursor == "" {
		return pgtype.Timestamptz{}, "", 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return pgtype.Timestamptz{}, "", 0, errBadCursor()
	}
	micros, rest, ok := strings.Cut(string(raw), ":")
	if !ok {
		return pgtype.Timestamptz{}, "", 0, errBadCursor()
	}
	kind, idPart, ok := strings.Cut(rest, ":")
	if !ok {
		return pgtype.Timestamptz{}, "", 0, errBadCursor()
	}
	us, err := strconv.ParseInt(micros, 10, 64)
	if err != nil {
		return pgtype.Timestamptz{}, "", 0, errBadCursor()
	}
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil {
		return pgtype.Timestamptz{}, "", 0, errBadCursor()
	}
	return pgtype.Timestamptz{Time: time.UnixMicro(us).UTC(), Valid: true}, kind, id, nil
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
