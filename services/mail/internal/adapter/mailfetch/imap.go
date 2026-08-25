// Package mailfetch pulls messages down over IMAP.
//
// A mail host gives no webhooks, so there is nothing to subscribe to: the only
// way to learn that a customer replied is to go and look. This adapter does
// the looking and hands raw RFC 5322 bytes upwards; nothing here parses,
// stores or interprets a message.
package mailfetch

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"

	"github.com/sgao19/erp-go/services/mail/internal/adapter/xoauth2"
	"github.com/sgao19/erp-go/services/mail/internal/app"
)

type IMAP struct {
	log     *slog.Logger
	timeout time.Duration
	// Separate from timeout. Establishing a TCP connection and waiting for a
	// command's answer are different kinds of wait: a host that is simply not
	// answering should be given up on in seconds, while a FETCH of a hundred
	// messages legitimately takes longer. One number for both meant a dead
	// mailbox held a worker for a command's worth of patience.
	dialTimeout time.Duration
	// Connections, kept between commands. See pool.go for why.
	pool *connPool
}

func NewIMAP(timeout, dialTimeout time.Duration, log *slog.Logger) *IMAP {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if dialTimeout <= 0 || dialTimeout > timeout {
		dialTimeout = 10 * time.Second
	}
	return &IMAP{
		log: log, timeout: timeout, dialTimeout: dialTimeout,
		// The pool's idle window is bounded by the command timeout, not chosen
		// independently of it — see idleWindow.
		pool: newConnPool(idleWindow(timeout)),
	}
}

// Fetch returns messages with a UID above sinceUID, newest last.
//
// The returned UIDValidity must be compared with what the caller stored: when
// it differs, every UID the caller holds refers to a different message or to
// nothing, and the only safe response is to resynchronise from zero. Silently
// carrying on is how a mailbox restore turns into permanently missed mail.
func (f *IMAP) Fetch(ctx context.Context, acct app.MailAccount, folder string, sinceUID uint32, limit uint32) (out app.FetchResult, err error) {

	c, err := f.borrow(acct)
	if err != nil {
		return out, err
	}
	// Released rather than logged out: the next command on this
	// mailbox reuses it. A failed command discards it instead.
	defer func() { f.release(acct, c, err) }()

	mbox, err := c.Select(folder, true) // read-only: syncing must not mark mail seen
	if err != nil {
		return out, fmt.Errorf("打开 %s 失败：%w", folder, err)
	}
	out.UIDValidity = mbox.UidValidity
	if mbox.Messages == 0 {
		return out, nil
	}

	// Ask which UIDs exist above the high-water mark before fetching any of
	// them. Fetching an open-ended range and stopping at a limit in the loop
	// makes the server stream the entire mailbox while we discard most of it,
	// which on a real mailbox ends in a read timeout rather than a result.
	all := new(imap.SeqSet)
	all.AddRange(sinceUID+1, 0)
	uids, err := c.UidSearch(&imap.SearchCriteria{Uid: all})
	if err != nil {
		return out, fmt.Errorf("查找新邮件失败：%w", err)
	}
	// X:* always matches the mailbox's newest message even when its UID is
	// below X — that is IMAP's rule, not a server quirk. Without this filter
	// every poll "finds" the newest mail again: storage dedupes it, but the
	// count reports one new message to somebody whose inbox has nothing new.
	fresh := uids[:0]
	for _, u := range uids {
		if u > sinceUID {
			fresh = append(fresh, u)
		}
	}
	uids = fresh
	if len(uids) == 0 {
		return out, nil
	}

	// A first sync starts near the top, not at the beginning of time. A
	// mailbox with years of history would otherwise spend its first hours
	// importing mail from before the company used this system, and the
	// message somebody is waiting to see would arrive last. History arrives
	// later, through FetchBelow, batch by batch.
	if sinceUID == 0 && uint32(len(uids)) > limit {
		uids = uids[uint32(len(uids))-limit:]
	} else if uint32(len(uids)) > limit {
		uids = uids[:limit]
	}

	out.Messages, err = f.fetchUIDs(c, uids)
	return out, err
}

// FetchBelow pulls history: the newest `limit` messages with UIDs strictly
// below `belowUID`. This is the backfill's engine — each pass reaches a
// little further into the past, newest first, so the history a person
// actually scrolls to arrives before the history nobody looks at.
func (f *IMAP) FetchBelow(ctx context.Context, acct app.MailAccount, folder string, belowUID uint32, limit uint32) (out app.FetchResult, err error) {
	if belowUID <= 1 {
		return out, nil
	}

	c, err := f.borrow(acct)
	if err != nil {
		return out, err
	}
	// Released rather than logged out: the next command on this
	// mailbox reuses it. A failed command discards it instead.
	defer func() { f.release(acct, c, err) }()

	mbox, err := c.Select(folder, true)
	if err != nil {
		return out, fmt.Errorf("打开 %s 失败：%w", folder, err)
	}
	out.UIDValidity = mbox.UidValidity
	if mbox.Messages == 0 {
		return out, nil
	}

	rng := new(imap.SeqSet)
	rng.AddRange(1, belowUID-1)
	uids, err := c.UidSearch(&imap.SearchCriteria{Uid: rng})
	if err != nil {
		return out, fmt.Errorf("查找历史邮件失败：%w", err)
	}
	if len(uids) == 0 {
		return out, nil
	}
	if uint32(len(uids)) > limit {
		uids = uids[uint32(len(uids))-limit:]
	}

	out.Messages, err = f.fetchUIDs(c, uids)
	return out, err
}

// FetchByUIDs retrieves specific messages whole. The refetch pass uses it to
// bring back originals lost to the raw-key collision; the UIDs it is handed
// come from a Message-ID search a moment earlier, so they are the folder's
// current numbering rather than whatever generation a stored UID belongs to.
func (f *IMAP) FetchByUIDs(ctx context.Context, acct app.MailAccount, folder string, uids []uint32) (out app.FetchResult, err error) {
	if folder == "" || len(uids) == 0 {
		return out, nil
	}
	c, err := f.borrow(acct)
	if err != nil {
		return out, err
	}
	// Released rather than logged out: the next command on this
	// mailbox reuses it. A failed command discards it instead.
	defer func() { f.release(acct, c, err) }()

	mbox, err := c.Select(folder, true) // read-only: repair must not mark mail seen
	if err != nil {
		return out, fmt.Errorf("打开 %s 失败：%w", folder, err)
	}
	out.UIDValidity = mbox.UidValidity
	out.Messages, err = f.fetchUIDs(c, uids)
	return out, err
}

// SentFolder finds where the host keeps sent mail.
func (f *IMAP) SentFolder(ctx context.Context, acct app.MailAccount) (string, error) {
	return f.specialFolder(acct, imap.SentAttr,
		[]string{"[Gmail]/Sent Mail", "Sent Items", "Sent Messages", "Sent", "已发送"},
		"找不到已发送文件夹")
}

// JunkFolder finds where the host keeps what it judged to be spam.
func (f *IMAP) JunkFolder(ctx context.Context, acct app.MailAccount) (string, error) {
	return f.specialFolder(acct, imap.JunkAttr,
		[]string{"[Gmail]/Spam", "Junk", "Junk E-mail", "Junk Email", "Spam", "垃圾邮件", "垃圾箱"},
		"找不到垃圾邮件文件夹")
}

// TrashFolder finds where the host keeps deleted mail. Every provider has
// one — deletion has to put things somewhere before they are purged.
func (f *IMAP) TrashFolder(ctx context.Context, acct app.MailAccount) (string, error) {
	return f.specialFolder(acct, imap.TrashAttr,
		[]string{"[Gmail]/Trash", "[Gmail]/&XfJT2ZZ8-", "Trash", "Deleted Items",
			"Deleted Messages", "已删除邮件", "已删除", "废件箱"},
		"找不到回收站文件夹")
}

// ArchiveFolder finds where archived mail goes, and returns "" when the host
// has no such place.
//
// Not every provider has the concept. Gmail does — archiving there means
// taking a message out of the inbox while it stays in All Mail, which is
// exactly the \All folder. A plain IMAP host like 263 has no archive at all,
// and inventing one by creating a folder in somebody's mailbox is not this
// program's decision to make. An empty name is the honest answer: the caller
// keeps the archive on the ERP side.
func (f *IMAP) ArchiveFolder(ctx context.Context, acct app.MailAccount) (string, error) {
	name, err := f.specialFolderOrEmpty(acct, imap.ArchiveAttr,
		[]string{"Archive", "Archives", "归档"})
	if err != nil || name != "" {
		return name, err
	}
	// Gmail's archive is "not in the inbox but still in All Mail".
	return f.specialFolderOrEmpty(acct, imap.AllAttr, []string{"[Gmail]/All Mail"})
}

// specialFolder locates a host's special-use folder.
//
// RFC 6154 gives folders attributes like \Sent and \Junk, and Gmail
// advertises them; other hosts predate the RFC and only have well-known
// names. Both are tried, in that order, because the attribute is
// authoritative and the names are guesses.
func (f *IMAP) specialFolder(acct app.MailAccount, attr string, guesses []string, missing string) (_ string, err error) {
	c, err := f.borrow(acct)
	if err != nil {
		return "", err
	}
	// Released rather than logged out: the next command on this
	// mailbox reuses it. A failed command discards it instead.
	defer func() { f.release(acct, c, err) }()

	boxes := make(chan *imap.MailboxInfo, 32)
	done := make(chan error, 1)
	go func() { done <- c.List("", "*", boxes) }()

	byAttr, names := "", map[string]bool{}
	for b := range boxes {
		names[b.Name] = true
		for _, a := range b.Attributes {
			if a == attr {
				byAttr = b.Name
			}
		}
	}
	if err := <-done; err != nil {
		return "", fmt.Errorf("列出文件夹失败：%w", err)
	}
	if byAttr != "" {
		return byAttr, nil
	}
	for _, guess := range guesses {
		if names[guess] {
			return guess, nil
		}
	}
	return "", fmt.Errorf("%s", missing)
}

// specialFolderOrEmpty is specialFolder for a folder that may legitimately
// not exist. A missing folder comes back as "" with no error; a failure to
// ask still comes back as an error, so "this host has none" and "we could
// not reach the host" never get confused for one another.
func (f *IMAP) specialFolderOrEmpty(acct app.MailAccount, attr string, guesses []string) (string, error) {
	name, err := f.specialFolder(acct, attr, guesses, "")
	if err != nil && name == "" && err.Error() == "" {
		return "", nil
	}
	if err != nil && err.Error() == "" {
		return "", nil
	}
	return name, err
}

// fetchUIDs downloads exactly these messages over an already-selected mailbox.
func (f *IMAP) fetchUIDs(c *client.Client, uids []uint32) ([]app.RawMessage, error) {
	seq := new(imap.SeqSet)
	for _, u := range uids {
		seq.AddNum(u)
	}

	section := &imap.BodySectionName{Peek: true} // Peek: do not set \Seen
	items := []imap.FetchItem{imap.FetchUid, imap.FetchInternalDate, imap.FetchFlags, section.FetchItem()}

	msgs := make(chan *imap.Message, 16)
	done := make(chan error, 1)
	go func() { done <- c.UidFetch(seq, items, msgs) }()

	var out []app.RawMessage
	for m := range msgs {
		body := m.GetBody(section)
		if body == nil {
			f.log.Warn("imap returned a message with no body", "uid", m.Uid)
			continue
		}
		raw, err := io.ReadAll(body)
		if err != nil {
			f.log.Warn("could not read a fetched message", "uid", m.Uid, "err", err)
			continue
		}
		seen := false
		for _, fl := range m.Flags {
			if fl == imap.SeenFlag {
				seen = true
			}
		}
		out = append(out, app.RawMessage{UID: m.Uid, Raw: raw, InternalDate: m.InternalDate, Seen: seen})
	}
	if err := <-done; err != nil {
		// Whatever arrived before the error is still good; the caller stores
		// it and the next pass resumes from where it left off.
		if len(out) == 0 {
			return nil, fmt.Errorf("收取邮件失败：%w", err)
		}
		f.log.Warn("imap fetch ended early", "got", len(out), "err", err)
	}
	return out, nil
}

// VerifyLogin proves the credentials against the live host and hangs up.
func (f *IMAP) VerifyLogin(ctx context.Context, acct app.MailAccount) error {
	c, err := f.dial(acct)
	if err != nil {
		return err
	}
	defer func() { _ = c.Logout() }()
	return f.login(c, acct)
}

// login authenticates by whichever door the account was bound through: a
// bearer token over XOAUTH2 for OAuth bindings, LOGIN for everything else.
func (f *IMAP) login(c *client.Client, acct app.MailAccount) error {
	if acct.AuthKind == "OAUTH" {
		if err := c.Authenticate(xoauth2.NewSASL(acct.Email, acct.Secret)); err != nil {
			return fmt.Errorf("Google 拒绝了访问令牌：%w", err)
		}
		return nil
	}
	if err := c.Login(acct.Login(), acct.Secret); err != nil {
		return fmt.Errorf("邮箱拒绝了这个授权码：%w", err)
	}
	return nil
}

func (f *IMAP) dial(acct app.MailAccount) (*client.Client, error) {
	addr := net.JoinHostPort(acct.IMAPHost, fmt.Sprint(acct.IMAPPort))
	if acct.IMAPHost == "" {
		return nil, fmt.Errorf("还没有配置 IMAP 服务器地址")
	}

	// The dialler carries the timeout. client.Timeout only applies once a
	// connection exists, so without this a host that accepts the TCP
	// connection and then says nothing hangs the sync for ever.
	d := &net.Dialer{Timeout: f.dialTimeout}

	var c *client.Client
	var err error
	switch acct.IMAPSecurity {
	case "SSL":
		c, err = client.DialWithDialerTLS(d, addr, &tls.Config{ServerName: acct.IMAPHost})
	case "STARTTLS":
		c, err = client.DialWithDialer(d, addr)
		if err == nil {
			err = c.StartTLS(&tls.Config{ServerName: acct.IMAPHost})
		}
	default:
		// Plaintext IMAP would put the authorisation code on the wire in
		// clear. Allowed only against a local test server, never a real host.
		c, err = client.DialWithDialer(d, addr)
	}
	if err != nil {
		return nil, fmt.Errorf("连接 %s 失败：%w", addr, err)
	}
	c.Timeout = f.timeout
	return c, nil
}

// WaitForNews holds an IDLE connection open until the folder changes, the
// wait expires, or the context ends. Returns true when something arrived.
//
// This is how "instant" mail clients are instant: the server pushes a bare
// "something changed" over a held-open connection, and the client then goes
// and fetches. For hosts without IDLE, go-imap quietly polls the same
// connection instead, so the caller cannot tell the difference — 263 works
// either way it is configured.
func (f *IMAP) WaitForNews(ctx context.Context, acct app.MailAccount, folder string, maxWait time.Duration) (bool, error) {
	c, err := f.dial(acct)
	if err != nil {
		return false, err
	}
	defer func() { _ = c.Logout() }()
	// The command timeout must not apply here: an idle connection is
	// legitimately silent for long stretches, and a 90-second read deadline
	// would kill every quiet wait. The IDLE restart below is what keeps the
	// connection provably alive instead.
	c.Timeout = 0

	if err := f.login(c, acct); err != nil {
		return false, err
	}
	if _, err := c.Select(folder, true); err != nil {
		return false, fmt.Errorf("打开 %s 失败：%w", folder, err)
	}

	updates := make(chan client.Update, 16)
	c.Updates = updates
	stop := make(chan struct{})
	var stopOnce sync.Once
	halt := func() { stopOnce.Do(func() { close(stop) }) }

	done := make(chan error, 1)
	go func() {
		// Restarting IDLE every 24 minutes stays under the RFC's 29-minute
		// server logout allowance with room to spare.
		done <- c.Idle(stop, &client.IdleOptions{LogoutTimeout: 24 * time.Minute})
	}()

	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	news := false
	for {
		select {
		case <-ctx.Done():
			halt()
		case <-timer.C:
			halt()
		case u := <-updates:
			// A mailbox update is new or expunged mail; either way the sync
			// should look. Other update kinds are connection chatter.
			if _, ok := u.(*client.MailboxUpdate); ok {
				news = true
				halt()
			}
		case err := <-done:
			if news {
				return true, nil
			}
			return false, err
		}
	}
}

// SetFlags publishes a flag change to the host, by UID.
//
// The whole batch shares one connection and one SELECT: a mailbox marked all
// read is one round trip, not one per message.
//
// SELECT is read-write here, unlike every other call in this adapter — this
// is the one place the ERP is allowed to change something in the real
// mailbox, and it changes exactly the flag it was asked to.
func (f *IMAP) SetFlags(ctx context.Context, acct app.MailAccount, folder string, uids []uint32, flag string, add bool) (err error) {
	if len(uids) == 0 {
		return nil
	}
	c, err := f.borrow(acct)
	if err != nil {
		return err
	}
	// Released rather than logged out: the next command on this
	// mailbox reuses it. A failed command discards it instead.
	defer func() { f.release(acct, c, err) }()

	if _, err := c.Select(folder, false); err != nil {
		return fmt.Errorf("打开 %s 失败：%w", folder, err)
	}

	set := new(imap.SeqSet)
	for _, u := range uids {
		set.AddNum(u)
	}
	op := imap.FlagsOp(imap.AddFlags)
	if !add {
		op = imap.RemoveFlags
	}
	// SILENT: we do not want the untagged FETCH responses back, and asking
	// for them on a large batch is a lot of traffic for something nobody
	// reads. The reconcile pass is what confirms the result.
	item := imap.FormatFlagsOp(op, true)
	if err := c.UidStore(set, item, []interface{}{flag}, nil); err != nil {
		return fmt.Errorf("写回标记失败：%w", err)
	}
	return nil
}

// FetchFlags reads back what the host currently believes about a set of
// messages. This is the other half of two-way sync: without it the ERP would
// publish its own changes and never learn about anybody else's.
func (f *IMAP) FetchFlags(ctx context.Context, acct app.MailAccount, folder string, uids []uint32) (_ map[uint32]app.MessageFlags, err error) {
	out := make(map[uint32]app.MessageFlags, len(uids))
	if len(uids) == 0 {
		return out, nil
	}
	c, err := f.borrow(acct)
	if err != nil {
		return nil, err
	}
	// Released rather than logged out: the next command on this
	// mailbox reuses it. A failed command discards it instead.
	defer func() { f.release(acct, c, err) }()

	if _, err := c.Select(folder, true); err != nil {
		return nil, fmt.Errorf("打开 %s 失败：%w", folder, err)
	}

	set := new(imap.SeqSet)
	for _, u := range uids {
		set.AddNum(u)
	}
	ch := make(chan *imap.Message, 64)
	done := make(chan error, 1)
	go func() {
		done <- c.UidFetch(set, []imap.FetchItem{imap.FetchUid, imap.FetchFlags}, ch)
	}()
	for m := range ch {
		var fl app.MessageFlags
		for _, name := range m.Flags {
			switch name {
			case imap.SeenFlag:
				fl.Seen = true
			case imap.FlaggedFlag:
				fl.Flagged = true
			}
		}
		out[m.Uid] = fl
	}
	if err := <-done; err != nil {
		return nil, fmt.Errorf("读取标记失败：%w", err)
	}
	return out, nil
}

// MoveMessages moves mail between folders on the host.
//
// go-imap falls back to COPY + \Deleted + EXPUNGE when the server has no MOVE
// extension, so this works on a plain IMAP host as well as on Gmail.
//
// The UIDs change in the destination and the host does not reliably say what
// they became, which is why nothing here tries to track them: anything that
// needs to find a moved message afterwards looks it up by Message-ID.
func (f *IMAP) MoveMessages(ctx context.Context, acct app.MailAccount, from string, uids []uint32, to string) (err error) {
	if len(uids) == 0 || to == "" {
		return nil
	}
	c, err := f.borrow(acct)
	if err != nil {
		return err
	}
	// Released rather than logged out: the next command on this
	// mailbox reuses it. A failed command discards it instead.
	defer func() { f.release(acct, c, err) }()
	if _, err := c.Select(from, false); err != nil {
		return fmt.Errorf("打开 %s 失败：%w", from, err)
	}
	set := new(imap.SeqSet)
	for _, u := range uids {
		set.AddNum(u)
	}
	if err := c.UidMove(set, to); err != nil {
		return fmt.Errorf("移动到 %s 失败：%w", to, err)
	}
	return nil
}

// AppendMessage files an already-sent message into a folder on the host.
//
// The counterpart to sending: SMTP relays a message and puts nothing in the
// sender's mailbox, so the copy in 已发送 has to be written over IMAP. See
// saveSentCopy in the app package for when this is and is not wanted.
//
// \Seen because nobody arrives at their own Sent folder to find out what they
// wrote; without the flag every send would raise an unread count on itself.
func (f *IMAP) AppendMessage(ctx context.Context, acct app.MailAccount, folder string, raw []byte, at time.Time) (err error) {
	if folder == "" || len(raw) == 0 {
		return nil
	}
	c, err := f.borrow(acct)
	if err != nil {
		return err
	}
	// Released rather than logged out: the next command on this
	// mailbox reuses it. A failed command discards it instead.
	defer func() { f.release(acct, c, err) }()
	if err := c.Append(folder, []string{imap.SeenFlag}, at, bytes.NewReader(raw)); err != nil {
		return fmt.Errorf("存入 %s 失败：%w", folder, err)
	}
	return nil
}

// FindUIDByMessageID locates one message in a folder by its Message-ID.
//
// The way to follow a message that has moved. A UID means nothing outside the
// folder it came from, but the Message-ID is the sender's own identifier and
// travels with the message wherever the host files it.
func (f *IMAP) FindUIDByMessageID(ctx context.Context, acct app.MailAccount, folder, messageID string) (_ uint32, _ bool, err error) {
	if messageID == "" || folder == "" {
		return 0, false, nil
	}
	c, err := f.borrow(acct)
	if err != nil {
		return 0, false, err
	}
	// Released rather than logged out: the next command on this
	// mailbox reuses it. A failed command discards it instead.
	defer func() { f.release(acct, c, err) }()
	if _, err := c.Select(folder, true); err != nil {
		return 0, false, fmt.Errorf("打开 %s 失败：%w", folder, err)
	}

	crit := imap.NewSearchCriteria()
	// Angle brackets restored: they are part of the header value, and a
	// server matching literally will not find the message without them.
	crit.Header.Add("Message-Id", asAngled(messageID))
	uids, err := c.UidSearch(crit)
	if err != nil {
		return 0, false, fmt.Errorf("在 %s 中查找失败：%w", folder, err)
	}
	if len(uids) == 0 {
		return 0, false, nil
	}
	// Newest match: a message can legitimately appear twice in a folder
	// after a failed move was retried.
	return uids[len(uids)-1], true, nil
}

// FindUIDsByMessageIDs locates many messages in one folder over a single
// connection.
//
// The searches still happen one per message — IMAP has no "find any of these
// Message-IDs" — but the expensive part was never the search. It was the
// dial, the TLS handshake and the authentication, repeated once per message
// and rejected by the host once a burst got long enough. Those happen once
// here.
func (f *IMAP) FindUIDsByMessageIDs(ctx context.Context, acct app.MailAccount, folder string, messageIDs []string) (_ map[string]uint32, err error) {
	out := make(map[string]uint32, len(messageIDs))
	if folder == "" || len(messageIDs) == 0 {
		return out, nil
	}
	c, err := f.borrow(acct)
	if err != nil {
		return nil, err
	}
	// Released rather than logged out: the next command on this
	// mailbox reuses it. A failed command discards it instead.
	defer func() { f.release(acct, c, err) }()
	if _, err := c.Select(folder, true); err != nil {
		return nil, fmt.Errorf("打开 %s 失败：%w", folder, err)
	}

	for _, id := range messageIDs {
		if id == "" {
			continue
		}
		// The context is checked between messages rather than inside the
		// library call: a batch can be long, and a shutdown should not have to
		// wait for the whole trash.
		if err := ctx.Err(); err != nil {
			return out, err
		}
		crit := imap.NewSearchCriteria()
		crit.Header.Add("Message-Id", asAngled(id))
		uids, err := c.UidSearch(crit)
		if err != nil {
			// One unanswerable search must not cost the rest of the batch. The
			// caller treats a missing id as "not here", which then takes the
			// careful one-at-a-time path.
			return out, fmt.Errorf("在 %s 中查找失败：%w", folder, err)
		}
		if len(uids) > 0 {
			// Newest match: a message can legitimately appear twice after a
			// failed move was retried.
			out[id] = uids[len(uids)-1]
		}
	}
	return out, nil
}

// SearchFlagged names every starred message in a folder, however old.
//
// The counterpart to FetchFlags, which can only answer about UIDs it is handed
// and so only ever sees the newest slice of a mailbox. SEARCH runs on the
// server over the whole folder and returns just the UIDs, so "which mail is
// starred" costs one round trip whether the folder holds fifty messages or
// fifty thousand — and a star put on a two-month-old thread in Gmail is found
// as readily as one put on this morning's.
func (f *IMAP) SearchFlagged(ctx context.Context, acct app.MailAccount, folder string) (_ []uint32, err error) {
	if folder == "" {
		return nil, nil
	}
	c, err := f.borrow(acct)
	if err != nil {
		return nil, err
	}
	// Released rather than logged out: the next command on this
	// mailbox reuses it. A failed command discards it instead.
	defer func() { f.release(acct, c, err) }()
	if _, err := c.Select(folder, true); err != nil {
		return nil, fmt.Errorf("打开 %s 失败：%w", folder, err)
	}

	crit := imap.NewSearchCriteria()
	crit.WithFlags = []string{imap.FlaggedFlag}
	uids, err := c.UidSearch(crit)
	if err != nil {
		return nil, fmt.Errorf("在 %s 中查找星标失败：%w", folder, err)
	}
	return uids, nil
}

// PurgeMessages deletes mail from the host for good.
//
// \Deleted then EXPUNGE, which is IMAP's only permanent deletion. There is no
// undo on the other side of this call — the safety net is that the ERP only
// ever asks for it about mail already sitting in its own trash, after the
// person confirmed.
func (f *IMAP) PurgeMessages(ctx context.Context, acct app.MailAccount, folder string, uids []uint32) (err error) {
	if len(uids) == 0 {
		return nil
	}
	c, err := f.borrow(acct)
	if err != nil {
		return err
	}
	// Released rather than logged out: the next command on this
	// mailbox reuses it. A failed command discards it instead.
	defer func() { f.release(acct, c, err) }()
	if _, err := c.Select(folder, false); err != nil {
		return fmt.Errorf("打开 %s 失败：%w", folder, err)
	}
	set := new(imap.SeqSet)
	for _, u := range uids {
		set.AddNum(u)
	}
	item := imap.FormatFlagsOp(imap.FlagsOp(imap.AddFlags), true)
	if err := c.UidStore(set, item, []interface{}{imap.DeletedFlag}, nil); err != nil {
		return fmt.Errorf("标记删除失败：%w", err)
	}
	if err := c.Expunge(nil); err != nil {
		return fmt.Errorf("清除失败：%w", err)
	}
	return nil
}

// asAngled restores the angle brackets stripped when the id was parsed.
func asAngled(id string) string {
	if id == "" || strings.HasPrefix(id, "<") {
		return id
	}
	return "<" + id + ">"
}

// RecentMessageIDs reads the Message-IDs of the newest messages in a folder.
//
// Used to work out where mail went when it stops appearing in the inbox: one
// pass over the trash and one over the archive answers that for every missing
// message at once, rather than a search per message.
func (f *IMAP) RecentMessageIDs(ctx context.Context, acct app.MailAccount, folder string, limit uint32) (_ map[string]bool, err error) {
	out := map[string]bool{}
	if folder == "" || limit == 0 {
		return out, nil
	}
	c, err := f.borrow(acct)
	if err != nil {
		return nil, err
	}
	// Released rather than logged out: the next command on this
	// mailbox reuses it. A failed command discards it instead.
	defer func() { f.release(acct, c, err) }()
	mbox, err := c.Select(folder, true)
	if err != nil {
		return nil, fmt.Errorf("打开 %s 失败：%w", folder, err)
	}
	if mbox.Messages == 0 {
		return out, nil
	}

	// The newest `limit` by sequence number. Envelopes only — the bodies are
	// not wanted and on All Mail would be the whole mailbox.
	from := uint32(1)
	if mbox.Messages > limit {
		from = mbox.Messages - limit + 1
	}
	seq := new(imap.SeqSet)
	seq.AddRange(from, mbox.Messages)

	ch := make(chan *imap.Message, 64)
	done := make(chan error, 1)
	go func() { done <- c.Fetch(seq, []imap.FetchItem{imap.FetchEnvelope}, ch) }()
	for m := range ch {
		if m.Envelope != nil && m.Envelope.MessageId != "" {
			out[strings.Trim(m.Envelope.MessageId, "<>")] = true
		}
	}
	if err := <-done; err != nil {
		return nil, fmt.Errorf("读取 %s 的信件标识失败：%w", folder, err)
	}
	return out, nil
}
