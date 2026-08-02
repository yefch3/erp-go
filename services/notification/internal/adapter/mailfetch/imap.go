// Package mailfetch pulls messages down over IMAP.
//
// A mail host gives no webhooks, so there is nothing to subscribe to: the only
// way to learn that a customer replied is to go and look. This adapter does
// the looking and hands raw RFC 5322 bytes upwards; nothing here parses,
// stores or interprets a message.
package mailfetch

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"

	"github.com/sgao19/erp-go/services/notification/internal/adapter/xoauth2"
	"github.com/sgao19/erp-go/services/notification/internal/app"
)

type IMAP struct {
	log     *slog.Logger
	timeout time.Duration
}

func NewIMAP(timeout time.Duration, log *slog.Logger) *IMAP {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &IMAP{log: log, timeout: timeout}
}

// Fetch returns messages with a UID above sinceUID, newest last.
//
// The returned UIDValidity must be compared with what the caller stored: when
// it differs, every UID the caller holds refers to a different message or to
// nothing, and the only safe response is to resynchronise from zero. Silently
// carrying on is how a mailbox restore turns into permanently missed mail.
func (f *IMAP) Fetch(ctx context.Context, acct app.MailAccount, folder string, sinceUID uint32, limit uint32) (app.FetchResult, error) {
	var out app.FetchResult

	c, err := f.dial(acct)
	if err != nil {
		return out, err
	}
	defer func() { _ = c.Logout() }()

	if err := f.login(c, acct); err != nil {
		return out, err
	}

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
func (f *IMAP) FetchBelow(ctx context.Context, acct app.MailAccount, folder string, belowUID uint32, limit uint32) (app.FetchResult, error) {
	var out app.FetchResult
	if belowUID <= 1 {
		return out, nil
	}

	c, err := f.dial(acct)
	if err != nil {
		return out, err
	}
	defer func() { _ = c.Logout() }()

	if err := f.login(c, acct); err != nil {
		return out, err
	}
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

// SentFolder finds where the host keeps sent mail.
//
// RFC 6154 gives folders a \Sent attribute and Gmail advertises it; other
// hosts predate the RFC and only have well-known names. Both are tried, in
// that order, because the attribute is authoritative and the names are
// guesses.
func (f *IMAP) SentFolder(ctx context.Context, acct app.MailAccount) (string, error) {
	c, err := f.dial(acct)
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Logout() }()
	if err := f.login(c, acct); err != nil {
		return "", err
	}

	boxes := make(chan *imap.MailboxInfo, 32)
	done := make(chan error, 1)
	go func() { done <- c.List("", "*", boxes) }()

	byAttr, names := "", map[string]bool{}
	for b := range boxes {
		names[b.Name] = true
		for _, a := range b.Attributes {
			if a == imap.SentAttr {
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
	for _, guess := range []string{"[Gmail]/Sent Mail", "Sent Items", "Sent Messages", "Sent", "已发送"} {
		if names[guess] {
			return guess, nil
		}
	}
	return "", fmt.Errorf("找不到已发送文件夹")
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
	d := &net.Dialer{Timeout: f.timeout}

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
