package mailfetch

import (
	"context"
	"sync"
	"time"

	"github.com/emersion/go-imap/client"

	"github.com/sgao19/erp-go/services/mail/internal/app"
)

// A connection per mailbox, kept open between commands.
//
// Every operation in this adapter used to dial its own connection, use it for
// one command and hang up: thirteen call sites, each doing TCP handshake, TLS
// handshake, LOGIN, one command, LOGOUT. Syncing one mailbox runs eight such
// operations — inbox new, inbox history, sent, sent history, junk, junk
// history, and two flag reconciliations — so a routine sync cost eight full
// handshakes to send eight commands.
//
// That is not how a mail client behaves. Thunderbird, Apple Mail and Outlook
// all hold a connection per account and issue commands down it; the handshake
// is paid once. The cost of not doing so is mostly latency, and it multiplies
// by the round trip: each operation spends about five round trips setting up
// and one doing the work, so at 200 ms to a distant host a sync spends eight
// seconds saying hello and one second fetching mail.
//
// So connections are kept. The call sites are unchanged — borrow() stands in
// for dial() and release() for Logout() — which is deliberate: thirteen places
// deciding for themselves when to reconnect is how the original shape came
// about.
//
// Three properties this has to hold, none of them optional:
//
//   - One command at a time per mailbox. go-imap's client is not safe for
//     concurrent use, and the poller and a person clicking 立即收信 can want
//     the same mailbox at the same moment. borrow() blocks until the previous
//     holder releases.
//   - A cached connection may already be dead. Hosts drop idle sessions
//     without saying so, and the failure surfaces as an error on the next
//     command rather than at borrow time. release() is told whether the
//     command failed and throws the connection away if it did, so the next
//     borrow dials fresh.
//   - IDLE never enters here. WaitForNews holds its connection for twenty
//     minutes with no read deadline; letting a fetch wait behind that would
//     turn "reuse the connection" into "block the sync for twenty minutes".

// idleEvictAfter is how long an unused connection is kept.
//
// Long enough to span the two-minute poll and a person working through their
// mail, short enough that a mailbox nobody touches is not holding a socket on
// somebody else's server all night. Hosts drop idle IMAP sessions somewhere
// around thirty minutes anyway; closing first is politer than being closed.
const idleEvictAfter = 10 * time.Minute

// conn is what the pool holds. An interface, not *client.Client, purely so
// the pool's own logic — which is concurrent and is where a bug would hide —
// can be tested without an IMAP server on the other end.
type conn interface{ Logout() error }

type pooled struct {
	c        conn
	inUse    bool
	lastUsed time.Time
	// ready is closed when the holder releases, so a waiter can be woken
	// without polling.
	ready chan struct{}
}

type connPool struct {
	mu    sync.Mutex
	conns map[int64]*pooled
}

func newConnPool() *connPool {
	return &connPool{conns: map[int64]*pooled{}}
}

// take claims the cached connection for an account, or reports that the
// caller must dial. It blocks while another command is using it.
//
// Returns nil when there is nothing usable cached; the caller dials and calls
// put() with the result.
func (p *connPool) take(accountID int64) conn {
	for {
		p.mu.Lock()
		e, ok := p.conns[accountID]
		if !ok {
			// Reserve the slot before releasing the lock, so two callers
			// racing on a cold mailbox do not both dial.
			p.conns[accountID] = &pooled{inUse: true, ready: make(chan struct{})}
			p.mu.Unlock()
			return nil
		}
		if !e.inUse {
			e.inUse = true
			c := e.c
			p.mu.Unlock()
			return c // may be nil: a reserved-but-not-yet-dialled slot
		}
		wait := e.ready
		p.mu.Unlock()
		<-wait // somebody else holds it; try again when they let go
	}
}

// put returns a connection to the pool, or discards it.
//
// A connection is discarded when the command that used it failed, because an
// IMAP error and a dead socket are indistinguishable from here and keeping a
// broken connection would make the next command fail too — turning one
// network blip into a mailbox that never syncs again.
func (p *connPool) put(accountID int64, c conn, failed bool) {
	p.mu.Lock()
	e, ok := p.conns[accountID]
	if !ok {
		p.mu.Unlock()
		if c != nil {
			_ = c.Logout()
		}
		return
	}
	if failed || c == nil {
		delete(p.conns, accountID)
		close(e.ready)
		p.mu.Unlock()
		if c != nil {
			_ = c.Logout()
		}
		return
	}
	e.c = c
	e.inUse = false
	e.lastUsed = time.Now()
	// A fresh channel for the next waiter; this one is closed to wake anybody
	// already queued.
	old := e.ready
	e.ready = make(chan struct{})
	p.mu.Unlock()
	close(old)
}

// evictIdle closes connections nobody has used lately. Returns how many went.
func (p *connPool) evictIdle(now time.Time) int {
	p.mu.Lock()
	var dead []conn
	for id, e := range p.conns {
		if e.inUse || now.Sub(e.lastUsed) < idleEvictAfter {
			continue
		}
		if e.c != nil {
			dead = append(dead, e.c)
		}
		delete(p.conns, id)
		close(e.ready)
	}
	p.mu.Unlock()
	for _, c := range dead {
		_ = c.Logout()
	}
	return len(dead)
}

// closeAll drops every connection, in use or not. For shutdown.
func (p *connPool) closeAll() {
	p.mu.Lock()
	var dead []conn
	for id, e := range p.conns {
		if e.c != nil {
			dead = append(dead, e.c)
		}
		delete(p.conns, id)
	}
	p.mu.Unlock()
	for _, c := range dead {
		_ = c.Logout()
	}
}

// borrow hands back a logged-in connection for this account, reusing the
// cached one when there is a live one and dialling when there is not.
//
// The caller must always call release, even on error, or the mailbox's slot
// stays claimed and every later command for it blocks for ever.
func (f *IMAP) borrow(acct app.MailAccount) (*client.Client, error) {
	if cached := f.pool.take(acct.AccountID); cached != nil {
		c := cached.(*client.Client)
		// The read deadline goes back on now that a command is about to run.
		// It is off while parked — see release, and the same reasoning
		// WaitForNews gives: a connection nobody is talking on is legitimately
		// silent, and a deadline against silence kills it.
		c.Timeout = f.timeout
		// One cheap round trip to find out whether the host is still there.
		// It usually is; when it is not, the alternative is handing a dead
		// connection to a fetch and reporting "打开 INBOX 失败" to somebody
		// whose mailbox is perfectly fine. A NOOP costs one round trip
		// against the handshake's five.
		if err := c.Noop(); err != nil {
			f.pool.put(acct.AccountID, nil, true)
			_ = c.Logout()
			// Fall through and dial: the slot is free again, and this call
			// still owes its caller a connection.
			return f.borrow(acct)
		}
		return c, nil
	}
	c, err := f.dial(acct)
	if err != nil {
		f.pool.put(acct.AccountID, nil, true)
		return nil, err
	}
	if err := f.login(c, acct); err != nil {
		f.pool.put(acct.AccountID, nil, true)
		_ = c.Logout()
		return nil, err
	}
	return c, nil
}

// release returns the connection. Pass the command's error: a failed command
// means the connection is suspect and is closed rather than handed on.
func (f *IMAP) release(acct app.MailAccount, c *client.Client, err error) {
	// Parked connections carry no read deadline. go-imap reads the connection
	// continuously in the background, so a deadline on an idle session is a
	// timer counting down to killing our own connection — which is exactly
	// what happened on the first deployment of this pool: the first sync was
	// twice as fast and the next one failed with "i/o timeout" against a
	// connection that had been sitting quiet for two minutes.
	if c != nil && err == nil {
		c.Timeout = 0
	}
	f.pool.put(acct.AccountID, c, err != nil)
}

// Run closes connections nobody has used lately, until the context ends.
//
// Without it the pool only ever grows: a mailbox synced once at midnight would
// hold a socket on its host until the process restarted. Hosts drop idle IMAP
// sessions on their own schedule anyway, and being dropped is worse than
// leaving — the drop surfaces as a failed command on some later sync.
func (f *IMAP) Run(ctx context.Context) {
	t := time.NewTicker(idleEvictAfter / 2)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			f.pool.closeAll()
			return
		case <-t.C:
			if n := f.pool.evictIdle(time.Now()); n > 0 {
				f.log.Info("closed idle mailbox connections", "count", n)
			}
		}
	}
}
