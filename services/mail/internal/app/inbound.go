package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// RawMessage is one message as the server holds it.
type RawMessage struct {
	UID          uint32
	Raw          []byte
	InternalDate time.Time
	// Seen is the host's own read flag. Backfilled history the person read
	// years ago in another client must not arrive here as five hundred
	// unread messages — that would bury the badge's actual signal.
	Seen bool
}

// FetchResult is one pass over a mailbox.
type FetchResult struct {
	UIDValidity uint32
	Messages    []RawMessage
}

// Mailbox is the inbound side of a mail host.
type Mailbox interface {
	// Fetch returns new mail: everything above sinceUID, capped at limit.
	Fetch(ctx context.Context, acct MailAccount, folder string, sinceUID uint32, limit uint32) (FetchResult, error)
	// FetchBelow returns history: the newest `limit` messages below belowUID.
	FetchBelow(ctx context.Context, acct MailAccount, folder string, belowUID uint32, limit uint32) (FetchResult, error)
	// SentFolder names the folder the host keeps sent mail in.
	SentFolder(ctx context.Context, acct MailAccount) (string, error)
	// JunkFolder names the folder the host files spam into.
	JunkFolder(ctx context.Context, acct MailAccount) (string, error)
	// VerifyLogin authenticates and disconnects: it proves the credentials
	// work today, and nothing else.
	VerifyLogin(ctx context.Context, acct MailAccount) error
	// SetFlags publishes a flag change to the host.
	SetFlags(ctx context.Context, acct MailAccount, folder string, uids []uint32, flag string, add bool) error
	// TrashFolder names the folder deleted mail goes to.
	TrashFolder(ctx context.Context, acct MailAccount) (string, error)
	// ArchiveFolder names where archived mail goes, or "" when this host has
	// no such place — a plain IMAP server has no archive concept at all.
	ArchiveFolder(ctx context.Context, acct MailAccount) (string, error)
	// MoveMessages moves mail between folders on the host.
	MoveMessages(ctx context.Context, acct MailAccount, from string, uids []uint32, to string) error
	// FindUIDByMessageID follows a message that has moved: its UID changed,
	// its Message-ID did not.
	FindUIDByMessageID(ctx context.Context, acct MailAccount, folder, messageID string) (uint32, bool, error)
	// FindUIDsByMessageIDs answers the same question for many messages over
	// one connection. Emptying a trash asks it once per mail, and a fresh
	// authenticated connection each time is what a host reads as abuse.
	FindUIDsByMessageIDs(ctx context.Context, acct MailAccount, folder string, messageIDs []string) (map[string]uint32, error)
	// PurgeMessages deletes mail from the host for good.
	PurgeMessages(ctx context.Context, acct MailAccount, folder string, uids []uint32) error
	// FetchFlags reads back what the host believes, so somebody else's
	// changes reach the ERP too. Bounded by the UIDs it is given.
	FetchFlags(ctx context.Context, acct MailAccount, folder string, uids []uint32) (map[uint32]MessageFlags, error)
	// SearchFlagged names every starred message in a folder, at any age.
	// FetchFlags cannot: it only answers about the UIDs handed to it, which
	// in practice is the newest few hundred.
	SearchFlagged(ctx context.Context, acct MailAccount, folder string) ([]uint32, error)
	// RecentMessageIDs names the newest messages of a folder, for working out
	// where mail went once it stops appearing in the inbox.
	RecentMessageIDs(ctx context.Context, acct MailAccount, folder string, limit uint32) (map[string]bool, error)
}

// MessageFlags is the host's view of one message.
type MessageFlags struct {
	Seen    bool
	Flagged bool
}

// UseMailbox installs the inbound adapter. Same two-step wiring as the
// sender, and for the same reason: the adapter needs the service to resolve
// credentials.
func (s *Service) UseMailbox(m Mailbox) { s.mailbox = m }

// SyncConfig tunes the inbound poller.
type SyncConfig struct {
	TenantID int64
	Folder   string
	Interval time.Duration
	// Ceiling on one pass, so a mailbox with ten years of history does not
	// take the service down on its first run.
	BatchSize uint32
	// How much history to hold per folder. The backfill walks the past one
	// batch per pass until this many messages are stored; 0 means the default.
	HistoryCap int64
}

func (c SyncConfig) withDefaults() SyncConfig {
	if c.TenantID == 0 {
		c.TenantID = 1
	}
	if c.Folder == "" {
		c.Folder = "INBOX"
	}
	if c.Interval <= 0 {
		c.Interval = 2 * time.Minute
	}
	if c.BatchSize == 0 {
		c.BatchSize = 50
	}
	if c.HistoryCap <= 0 {
		c.HistoryCap = 500
	}
	return c
}

// RunInboundSync polls every configured mailbox for ever.
//
// Polling rather than pushing because a mail host offers nothing else. The
// interval is the honest cost of that: a reply is visible within one cycle,
// not instantly, and pretending otherwise would mean holding an IDLE
// connection open per employee.
func (s *Service) RunInboundSync(ctx context.Context, cfg SyncConfig) {
	cfg = cfg.withDefaults()
	if s.mailbox == nil {
		s.log.Info("inbound sync not started — no mailbox adapter configured")
		return
	}
	s.log.Info("inbound sync started", "folder", cfg.Folder, "every", cfg.Interval)

	t := time.NewTicker(cfg.Interval)
	defer t.Stop()
	for {
		s.syncAllMailboxes(ctx, cfg)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) syncAllMailboxes(ctx context.Context, cfg SyncConfig) {
	accounts, err := s.q.ListSyncableMailAccounts(ctx, cfg.TenantID)
	if err != nil {
		s.log.Error("could not list mailboxes to sync", "err", err)
		return
	}
	for _, a := range accounts {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// One mailbox failing must not stop the rest: a single employee's
		// expired authorisation code should not stop the whole company
		// receiving mail.
		if n, err := s.SyncMailbox(ctx, cfg, a.EmployeeID); err != nil {
			s.log.Warn("mailbox sync failed", "employee", a.EmployeeID, "err", err)
		} else if n > 0 {
			s.log.Info("mailbox synced", "employee", a.EmployeeID, "new", n)
		}
	}
}

// SyncMailbox pulls one mailbox — new mail first, then a slice of history —
// and returns how many new INBOX messages it stored.
func (s *Service) SyncMailbox(ctx context.Context, cfg SyncConfig, employeeID int64) (int, error) {
	cfg = cfg.withDefaults()
	acct, err := s.ForSender(ctx, cfg.TenantID, employeeID)
	if err != nil {
		return 0, err
	}

	newInbox, err := s.syncFolder(ctx, cfg, acct, "INBOX", "INBOX")
	if err != nil {
		_ = s.q.MarkSyncFailed(ctx, store.MarkSyncFailedParams{
			TenantID: cfg.TenantID, AccountID: acct.AccountID, Folder: "INBOX",
			LastError: err.Error(),
		})
		// Also onto the account, which is what the mailbox page reads. A
		// revoked authorisation used to fail here every two minutes for as
		// long as it took somebody to notice, while the page went on showing
		// the last successful sync as though it were current. Silence is the
		// bug: the mailbox has to be able to say it is not receiving.
		s.RecordFailure(ctx, cfg.TenantID, acct.AccountID, err.Error())
		return 0, err
	}
	// Cleared on the way back up, so a recovered mailbox stops complaining
	// without anybody having to sign in again.
	s.clearFailure(ctx, cfg.TenantID, acct.AccountID)

	// Sent history rides along on the same pass. A failure here is logged and
	// does not fail the sync: the inbox is what somebody is waiting on.
	if actual, err := s.specialFolderOf(ctx, acct, "sent"); err != nil {
		s.log.Warn("could not locate the sent folder", "account", acct.AccountID, "err", err)
	} else if _, err := s.syncFolder(ctx, cfg, acct, "SENT", actual); err != nil {
		s.log.Warn("sent-folder sync failed", "account", acct.AccountID, "err", err)
	}

	// The junk folder too — read-only safety net for the false positive: the
	// customer inquiry the host wrongly filed as spam would otherwise be
	// invisible to somebody using this as their only client. Shallower
	// history than the inbox: old spam is the least valuable mail there is.
	jcfg := cfg
	if jcfg.HistoryCap > 100 {
		jcfg.HistoryCap = 100
	}
	if actual, err := s.specialFolderOf(ctx, acct, "junk"); err != nil {
		s.log.Warn("could not locate the junk folder", "account", acct.AccountID, "err", err)
	} else if _, err := s.syncFolder(ctx, jcfg, acct, "JUNK", actual); err != nil {
		s.log.Warn("junk-folder sync failed", "account", acct.AccountID, "err", err)
	}

	// The host's own read state, taken back over the newest slice of the
	// inbox. This is the half of two-way sync that carries somebody else's
	// Gmail session into the ERP; it no-ops while local changes are still
	// queued, so it can never overwrite one on its way up.
	if err := s.ReconcileFlags(ctx, cfg.TenantID, acct, "INBOX", "INBOX", true); err != nil {
		s.log.Warn("could not reconcile the inbox", "account", acct.AccountID, "err", err)
	}
	// The junk folder gets its flags reconciled too, but not its departures:
	// mail leaves spam mostly because somebody rescued it, and calling that a
	// deletion would bin the message they just saved.
	if actual, err := s.specialFolderOf(ctx, acct, "junk"); err == nil {
		if err := s.ReconcileFlags(ctx, cfg.TenantID, acct, "JUNK", actual, false); err != nil {
			s.log.Warn("could not reconcile the junk folder", "account", acct.AccountID, "err", err)
		}
	}

	// The ping goes out only after everything is committed, and only to the
	// mailbox owner: an inbox is personal.
	if newInbox > 0 && s.live != nil {
		s.live.ToEmployees(ctx, cfg.TenantID, []int64{employeeID},
			livefeed.Event{Type: livefeed.MailInbound})
	}
	return newInbox, nil
}

// syncFolder brings one folder up to date and reaches one batch further into
// its history. `logical` is our stable name (INBOX/SENT) used in storage;
// `actual` is whatever the host calls the folder.
func (s *Service) syncFolder(ctx context.Context, cfg SyncConfig, acct MailAccount, logical, actual string) (int, error) {
	state, err := s.q.GetSyncState(ctx, store.GetSyncStateParams{
		TenantID: cfg.TenantID, AccountID: acct.AccountID, Folder: logical,
	})
	if err != nil {
		state = store.GetSyncStateRow{} // never synced
	}

	res, err := s.mailbox.Fetch(ctx, acct, actual, uint32(state.LastUid), cfg.BatchSize)
	if err != nil {
		return 0, err
	}

	// UIDVALIDITY changing means every UID we hold now refers to something
	// else, or to nothing. Start over: the high and low marks are both
	// meaningless against the new numbering.
	if state.UidValidity != 0 && uint32(state.UidValidity) != res.UIDValidity {
		s.log.Warn("mailbox UIDVALIDITY changed, resynchronising",
			"account", acct.AccountID, "folder", logical,
			"was", state.UidValidity, "now", res.UIDValidity)
		state = store.GetSyncStateRow{}
		res, err = s.mailbox.Fetch(ctx, acct, actual, 0, cfg.BatchSize)
		if err != nil {
			return 0, err
		}
	}

	stored := 0
	highest := uint32(state.LastUid)
	lowest := uint32(state.LowUid)
	ingestBatch := func(msgs []RawMessage) {
		for _, m := range msgs {
			if err := s.ingest(ctx, cfg.TenantID, acct, logical, m); err != nil {
				s.log.Warn("could not store a message",
					"account", acct.AccountID, "folder", logical, "uid", m.UID, "err", err)
				// The high-water mark still advances: a message we cannot
				// parse must not be retried on every pass for ever.
			} else {
				stored++
			}
			if m.UID > highest {
				highest = m.UID
			}
			if lowest == 0 || m.UID < lowest {
				lowest = m.UID
			}
		}
	}
	ingestBatch(res.Messages)

	// One batch of history per pass, newest first, until the cap. low_uid==1
	// marks the bottom: the dial for "is there anything older" is not free.
	if lowest > 1 {
		held, err := s.q.CountFolder(ctx, store.CountFolderParams{
			TenantID: cfg.TenantID, AccountID: acct.AccountID, Folder: logical,
		})
		if err == nil && held < cfg.HistoryCap {
			old, err := s.mailbox.FetchBelow(ctx, acct, actual, lowest, cfg.BatchSize)
			if err != nil {
				s.log.Warn("history backfill failed",
					"account", acct.AccountID, "folder", logical, "err", err)
			} else if len(old.Messages) == 0 {
				lowest = 1 // bottom reached; stop dialling for more
			} else {
				ingestBatch(old.Messages)
			}
		}
	}

	if err := s.q.UpsertSyncState(ctx, store.UpsertSyncStateParams{
		TenantID: cfg.TenantID, AccountID: acct.AccountID, Folder: logical,
		UidValidity: int64(res.UIDValidity), LastUid: int64(highest), LowUid: int64(lowest),
	}); err != nil {
		s.log.Error("could not record sync progress", "account", acct.AccountID, "err", err)
	}
	return stored, nil
}

// specialFolderOf resolves and remembers where this account keeps a special
// folder ("sent" or "junk"). Cached because the answer never changes and
// finding it costs a dial.
func (s *Service) specialFolderOf(ctx context.Context, acct MailAccount, kind string) (string, error) {
	key := fmt.Sprintf("%s:%d", kind, acct.AccountID)
	if v, ok := s.sentFolders.Load(key); ok {
		return v.(string), nil
	}
	var name string
	var err error
	switch kind {
	case "junk":
		name, err = s.mailbox.JunkFolder(ctx, acct)
	case "trash":
		name, err = s.mailbox.TrashFolder(ctx, acct)
	case "archive":
		// May legitimately be empty: not every host has an archive. Cached
		// either way, so a host without one is not asked again every time
		// somebody archives something.
		name, err = s.mailbox.ArchiveFolder(ctx, acct)
	default:
		name, err = s.mailbox.SentFolder(ctx, acct)
	}
	if err != nil {
		return "", err
	}
	s.sentFolders.Store(key, name)
	return name, nil
}

// ingest parses one message and files it.
func (s *Service) ingest(ctx context.Context, tenantID int64, acct MailAccount, folder string, m RawMessage) error {
	parsed, err := ParseMail(m.Raw)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	sentAt := parsed.SentAt
	if sentAt.IsZero() {
		sentAt = m.InternalDate
	}
	// When the host says it arrived. Its own clock, not ours and not the
	// sender's: the Date header is written by whoever sent the mail and can
	// be wrong by accident or on purpose, while INTERNALDATE is the one
	// timestamp the mailbox itself stands behind.

	// The raw MIME goes to object storage before the row exists: a row that
	// promises a raw_key which was never written is worse than no row.
	rawKey := fmt.Sprintf("mail/inbound/%d/%d/%d.eml", tenantID, acct.AccountID, m.UID)
	if s.files != nil {
		if err := s.putRaw(ctx, rawKey, m.Raw); err != nil {
			s.log.Warn("could not store raw message, keeping the parsed copy only",
				"uid", m.UID, "err", err)
			rawKey = ""
		}
	} else {
		rawKey = ""
	}

	// The host saves a copy of everything sent over SMTP into the sent
	// folder — including what the ERP itself sent. Those already exist as
	// campaign messages with per-recipient status; storing the copy would
	// show every ERP send twice.
	if folder == "SENT" {
		if key := messageKeyFromID(parsed.MessageID); key != "" {
			if _, err := s.q.FindMessageByKey(ctx, store.FindMessageByKeyParams{
				TenantID: tenantID, MessageKey: key,
			}); err == nil {
				return nil
			}
		}
	}

	threadKey, replyTo := s.resolveThread(ctx, tenantID, parsed)

	id, err := s.q.InsertInbound(ctx, store.InsertInboundParams{
		TenantID: tenantID, AccountID: acct.AccountID, OwnerID: acct.EmployeeID,
		Folder: folder, ImapUid: int64(m.UID),
		MessageID: parsed.MessageID, InReplyTo: parsed.InReplyTo,
		ReferencesIds: strings.Join(parsed.References, " "),
		ThreadKey:     threadKey, ReplyToID: replyTo,
		FromEmail: parsed.FromEmail, FromName: parsed.FromName,
		ToEmail: parsed.ToEmail, Subject: parsed.Subject,
		BodyHtml: parsed.BodyHTML, BodyText: parsed.BodyText,
		Snippet: snippetOf(parsed), RawKey: rawKey, RawSize: int64(len(m.Raw)),
		IsBounce: parsed.IsBounce, HasAttachments: len(parsed.Attachments) > 0,
		IsRead: m.Seen,
		SentAt: pgtype.Timestamptz{Time: sentAt, Valid: !sentAt.IsZero()},
		ReceivedAt: pgtype.Timestamptz{
			Time: m.InternalDate, Valid: !m.InternalDate.IsZero(),
		},
	})
	if err != nil {
		// ON CONFLICT DO NOTHING returns no row at all, which sqlc surfaces as
		// "no rows in result set". That is the normal outcome of re-fetching a
		// UID we already hold, not a failure — reporting it as one filled the
		// log with warnings about messages that were stored correctly.
		if errors.Is(err, pgx.ErrNoRows) || strings.Contains(err.Error(), "duplicate key") {
			return nil
		}
		return err
	}

	for _, a := range parsed.Attachments {
		key := fmt.Sprintf("mail/inbound/%d/%d/att/%d-%s", tenantID, acct.AccountID, id, safeName(a.FileName))
		if s.files != nil {
			if err := s.putRaw(ctx, key, a.Data); err != nil {
				s.log.Warn("could not store an incoming attachment", "file", a.FileName, "err", err)
				key = ""
			}
		}
		if err := s.q.InsertInboundAttachment(ctx, store.InsertInboundAttachmentParams{
			TenantID: tenantID, InboundID: id, FileName: a.FileName,
			ContentType: a.ContentType, FileSize: int64(len(a.Data)), FileKey: key,
		}); err != nil {
			s.log.Warn("could not record an incoming attachment", "file", a.FileName, "err", err)
		}
	}

	// Bounce side effects only from the inbox. A bounce-shaped message the
	// host itself filed as spam must not be allowed to suppress an address.
	if parsed.IsBounce && folder == "INBOX" {
		s.applyBounce(ctx, tenantID, m.Raw, parsed)
	}
	return nil
}

// resolveThread ties a reply to the message that prompted it.
//
// Our own Message-IDs are <message_key@domain> by construction, so any id in
// In-Reply-To or References that we recognise identifies the exact send being
// answered. That is what puts a customer's reply underneath the quotation it
// concerns rather than loose in a list.
func (s *Service) resolveThread(ctx context.Context, tenantID int64, p ParsedMail) (string, *int64) {
	candidates := append([]string{p.InReplyTo}, p.References...)
	for _, id := range candidates {
		key := messageKeyFromID(id)
		if key == "" {
			continue
		}
		row, err := s.q.FindMessageByKey(ctx, store.FindMessageByKeyParams{
			TenantID: tenantID, MessageKey: key,
		})
		if err != nil {
			continue
		}
		msgID := row.ID
		thread := row.ThreadKey
		if thread == "" {
			thread = row.MessageKey
		}
		return thread, &msgID
	}
	// Not ours. Keep the sender's own chain together: the root of References
	// is the start of their conversation, and failing that the message is the
	// start of its own.
	if len(p.References) > 0 {
		return truncate(p.References[0], 64), nil
	}
	if p.InReplyTo != "" {
		return truncate(p.InReplyTo, 64), nil
	}
	return truncate(p.MessageID, 64), nil
}

// messageKeyFromID extracts our own key out of a Message-ID we issued.
func messageKeyFromID(id string) string {
	id = strings.Trim(strings.TrimSpace(id), "<>")
	i := strings.Index(id, "@")
	if i <= 0 {
		return ""
	}
	return id[:i]
}

// applyBounce turns a delivery report into a fact about a recipient.
//
// This is the only bounce signal a mail host gives us — there is no webhook —
// so without it the needs-attention queue stays empty while mail quietly
// fails to arrive.
func (s *Service) applyBounce(ctx context.Context, tenantID int64, raw []byte, p ParsedMail) {
	for _, origID := range OriginalMessageIDs(raw) {
		key := messageKeyFromID(origID)
		if key == "" {
			continue
		}
		row, err := s.q.FindMessageByKey(ctx, store.FindMessageByKeyParams{
			TenantID: tenantID, MessageKey: key,
		})
		if err != nil {
			continue
		}
		status := "NEEDS_ATTENTION"
		reason := "对方服务器暂时退信：" + p.BounceDetail
		if p.BouncePermanent {
			status = "HARD_BOUNCED"
			reason = "地址不可达，已加入拒收名单：" + p.BounceDetail
		}
		if err := s.q.MarkTerminal(ctx, store.MarkTerminalParams{
			TenantID: tenantID, ID: row.ID, Status: status,
			LastError: p.BounceDetail, AttentionReason: reason,
		}); err != nil {
			s.log.Warn("could not record a bounce", "message", row.ID, "err", err)
			continue
		}
		// Only a permanent failure suppresses. A full mailbox or a greylist
		// is temporary, and blacklisting a customer over one would quietly
		// end the correspondence.
		if p.BouncePermanent {
			addr := p.BounceRecipient
			if addr == "" {
				addr = row.ToEmail
			}
			if addr != "" {
				_ = s.Suppress(ctx, tenantID, addr, "HARD_BOUNCE", p.BounceDetail)
			}
		}
		s.log.Info("bounce applied", "message", row.ID, "permanent", p.BouncePermanent)
	}
}

func (s *Service) putRaw(ctx context.Context, key string, data []byte) error {
	return s.files.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "message/rfc822")
}

// snippetOf is the one line of the mail the list shows beside the subject.
//
// The text part is preferred but not trusted: plenty of senders put markup in
// text/plain, and one that does would put "<!doctype html><html xmlns=..." in
// front of the person instead of the first sentence. Anything that looks like
// markup goes through the HTML path, which strips <style> and <script> bodies
// rather than just their tags — a mail's stylesheet is otherwise the first
// thing in the snippet and the longest.
func snippetOf(p ParsedMail) string {
	text := p.BodyText
	if strings.TrimSpace(text) == "" || looksLikeMarkup(text) {
		html := p.BodyHTML
		if strings.TrimSpace(html) == "" {
			html = text
		}
		text = HTMLToText(html)
	}
	text = strings.Join(strings.Fields(text), " ")
	return truncate(text, 280)
}

// markupHead spots the openings that mean "this is a document, not a
// sentence". Deliberately anchored near the start: a plain-text mail that
// happens to mention <b> further down is still plain text.
var markupHead = regexp.MustCompile(`(?is)^\s*(<!doctype|<html|<head|<body|<table|<div|<style|<meta)`)

func looksLikeMarkup(s string) bool { return markupHead.MatchString(s) }

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func safeName(s string) string {
	repl := strings.NewReplacer("/", "_", "\\", "_", "..", "_", " ", "_")
	return truncate(repl.Replace(s), 80)
}

// NewsWaiter is the optional push side of a mailbox: an adapter that can hold
// a connection open and report "something changed" the moment it does.
// Optional by type assertion so fakes and future adapters need not fake it.
type NewsWaiter interface {
	WaitForNews(ctx context.Context, acct MailAccount, folder string, maxWait time.Duration) (bool, error)
}

// RunIdleWatchers keeps one held-open IMAP connection per active mailbox and
// syncs the moment the host reports new mail.
//
// This is the difference between "within two minutes" and "within seconds":
// the poller stays as the safety net and the historian (sent folder,
// backfill), while the watcher makes the inbox feel live. One goroutine and
// one connection per mailbox is the honest cost.
func (s *Service) RunIdleWatchers(ctx context.Context, cfg SyncConfig) {
	cfg = cfg.withDefaults()
	waiter, ok := s.mailbox.(NewsWaiter)
	if !ok {
		s.log.Info("mailbox adapter cannot push; relying on the poller alone")
		return
	}
	s.log.Info("idle watchers started")

	var mu sync.Mutex
	running := map[int64]bool{}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		accounts, err := s.q.ListSyncableMailAccounts(ctx, cfg.TenantID)
		if err != nil {
			s.log.Warn("could not list mailboxes to watch", "err", err)
		}
		for _, a := range accounts {
			mu.Lock()
			already := running[a.EmployeeID]
			if !already {
				running[a.EmployeeID] = true
			}
			mu.Unlock()
			if already {
				continue
			}
			go func(emp int64) {
				defer func() {
					mu.Lock()
					delete(running, emp)
					mu.Unlock()
				}()
				s.watchMailbox(ctx, cfg, waiter, emp)
			}(a.EmployeeID)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) watchMailbox(ctx context.Context, cfg SyncConfig, waiter NewsWaiter, employeeID int64) {
	backoff := time.Minute
	for ctx.Err() == nil {
		acct, err := s.ForSender(ctx, cfg.TenantID, employeeID)
		if err != nil {
			// Unbound or paused. The manager restarts the watch if the
			// account comes back; holding a loop open for it helps nobody.
			return
		}
		news, err := waiter.WaitForNews(ctx, acct, "INBOX", 25*time.Minute)
		if err != nil {
			s.log.Warn("idle watch dropped", "employee", employeeID, "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			// A host that keeps refusing gets visited less and less often;
			// the poller still covers the gap.
			if backoff < 10*time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = time.Minute
		if news {
			if n, err := s.SyncMailbox(ctx, cfg, employeeID); err != nil {
				s.log.Warn("push-triggered sync failed", "employee", employeeID, "err", err)
			} else if n > 0 {
				s.log.Info("mail arrived by push", "employee", employeeID, "new", n)
			}
		}
	}
}
