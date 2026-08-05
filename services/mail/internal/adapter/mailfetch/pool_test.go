package mailfetch

import (
	"sync"
	"testing"
	"time"
)

// fakeConn stands in for an IMAP connection so the pool's own logic can be
// tested. The pool is the only concurrent code in this adapter and the only
// place holding state between commands, which makes it the only place a bug
// here would be hard to see.
type fakeConn struct {
	name       string
	mu         sync.Mutex
	loggedOut  bool
	logoutSeen int
}

func (f *fakeConn) Logout() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loggedOut = true
	f.logoutSeen++
	return nil
}

func (f *fakeConn) closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loggedOut
}

func TestPoolReusesAConnectionInsteadOfDiallingAgain(t *testing.T) {
	p := newConnPool()
	c := &fakeConn{name: "first"}

	// Cold mailbox: nothing cached, so the caller must dial.
	if got := p.take(1); got != nil {
		t.Fatalf("a cold mailbox handed back %v, want nil so the caller dials", got)
	}
	p.put(1, c, false)

	// The whole point: the second command gets the first command's connection.
	got := p.take(1)
	if got != conn(c) {
		t.Fatalf("the second command did not get the first command's connection: got %v\n"+
			"without this every IMAP operation pays a fresh TCP + TLS + LOGIN", got)
	}
	if c.closed() {
		t.Error("the connection was logged out on release; it must stay open for reuse")
	}
	p.put(1, c, false)
}

func TestPoolThrowsAwayAConnectionWhoseCommandFailed(t *testing.T) {
	p := newConnPool()
	bad := &fakeConn{name: "bad"}

	p.take(1)
	p.put(1, bad, true) // the command errored

	if !bad.closed() {
		t.Error("a connection whose command failed was not closed")
	}
	// And the next borrow must dial rather than hand back the suspect one: a
	// dead socket and an IMAP error look identical from here, so keeping it
	// would turn one network blip into a mailbox that never syncs again.
	if got := p.take(1); got != nil {
		t.Fatalf("a failed connection was handed to the next command: %v", got)
	}
}

func TestPoolLetsOnlyOneCommandUseAMailboxAtATime(t *testing.T) {
	p := newConnPool()
	c := &fakeConn{name: "shared"}
	p.take(1)
	p.put(1, c, false)

	// go-imap's client is not safe for concurrent use, and the poller and a
	// person clicking 立即收信 can want the same mailbox at the same moment.
	var mu sync.Mutex
	concurrent, peak := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := p.take(1)
			mu.Lock()
			concurrent++
			if concurrent > peak {
				peak = concurrent
			}
			mu.Unlock()

			time.Sleep(time.Millisecond) // hold it, as a real command would

			mu.Lock()
			concurrent--
			mu.Unlock()
			p.put(1, got, false)
		}()
	}
	wg.Wait()

	if peak != 1 {
		t.Fatalf("%d commands held the same mailbox's connection at once; go-imap's client is not concurrency-safe", peak)
	}
}

func TestPoolDoesNotSerialiseDifferentMailboxes(t *testing.T) {
	p := newConnPool()
	// Two mailboxes must not queue behind each other — that would put the
	// sequential sync back, which is the thing this exists to end.
	done := make(chan struct{})
	go func() {
		p.take(1)
		<-done // holds mailbox 1 open
		p.put(1, &fakeConn{}, false)
	}()
	time.Sleep(5 * time.Millisecond)

	got := make(chan struct{})
	go func() {
		p.take(2)
		close(got)
	}()
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("a second mailbox blocked behind the first")
	}
	close(done)
}

func TestPoolClosesConnectionsNobodyIsUsing(t *testing.T) {
	p := newConnPool()
	idle := &fakeConn{name: "idle"}
	busy := &fakeConn{name: "busy"}

	p.take(1)
	p.put(1, idle, false)
	p.take(2) // claimed and never released: in use

	n := p.evictIdle(time.Now().Add(idleEvictAfter + time.Minute))
	if n != 1 {
		t.Fatalf("evicted %d, want 1", n)
	}
	if !idle.closed() {
		t.Error("an idle connection was left holding a socket on somebody else's server")
	}
	if busy.closed() {
		t.Error("a connection in the middle of a command was closed under it")
	}
	// Evicting must leave the slot dialable rather than deadlocked.
	if got := p.take(1); got != nil {
		t.Fatal("after eviction the mailbox should need a fresh dial")
	}
}
