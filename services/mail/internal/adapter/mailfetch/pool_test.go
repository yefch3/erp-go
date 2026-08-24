package mailfetch

import (
	"sync"
	"testing"
	"time"
)

// testIdleWindow is a stand-in for the window the real pool derives from the
// command timeout. These tests drive evictIdle with an explicit clock, so the
// value only has to be non-zero.
const testIdleWindow = 10 * time.Minute

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
	p := newConnPool(testIdleWindow)
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
	p := newConnPool(testIdleWindow)
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
	p := newConnPool(testIdleWindow)
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
	p := newConnPool(testIdleWindow)
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
	p := newConnPool(testIdleWindow)
	idle := &fakeConn{name: "idle"}
	busy := &fakeConn{name: "busy"}

	p.take(1)
	p.put(1, idle, false)
	p.take(2) // claimed and never released: in use

	n := p.evictIdle(time.Now().Add(testIdleWindow + time.Minute))
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

// 停在池子里的连接，必须在它自己的读截止时间到期之前被收走。
//
// 这条不是调优，是正确性。go-imap 只在 execute() 里碰套接字的截止时间，所以
// 上一条命令设下的那个会在没人说话的连接上继续倒数；停留超过命令超时，库的
// 后台读取协程就会撞上它，打一行 "error reading response: i/o timeout" 然后
// 把连接扔掉——而活早就干完了，所以什么都没坏，只有满屏看着像故障的日志。
//
// 生产上就是这样：每两分钟一簇，一次不落，持续了很久没人发现。
func TestIdleWindowStaysUnderTheCommandTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{
		30 * time.Second,
		90 * time.Second, // MAIL_SYNC_TIMEOUT 的默认值
		5 * time.Minute,
		time.Hour, // 长超时下由 maxIdleWindow 封顶，不是由超时封顶
	} {
		w := idleWindow(timeout)
		if w >= timeout {
			t.Fatalf("超时 %s 时窗口 %s：连接会死在自己的截止时间上", timeout, w)
		}
		if w > maxIdleWindow {
			t.Fatalf("超时 %s 时窗口 %s 超过上限 %s", timeout, w, maxIdleWindow)
		}
		if w <= 0 {
			t.Fatalf("超时 %s 时窗口 %s：池子等于没了", timeout, w)
		}
		// 收割的滴答也要赶在窗口之内，否则「关在前面」只是纸面上的。
		if tick := w / 3; w+tick >= timeout {
			t.Fatalf("超时 %s：窗口 %s 加一次迟到的滴答 %s 就越过截止时间了", timeout, w, tick)
		}
	}
}

// 归还连接时压低 Timeout，是为了让收割时发出的 LOGOUT 有个界，
// 不是为了解掉截止时间——那件事这行做不到。
func TestLogoutTimeoutIsBounded(t *testing.T) {
	if logoutTimeout <= 0 {
		t.Fatal("LOGOUT 没有上界：对方收下连接又不吭声就能卡住收割协程")
	}
	if logoutTimeout > idleWindow(90*time.Second) {
		t.Fatalf("LOGOUT 的上界 %s 比闲置窗口还长，收割会拖住自己", logoutTimeout)
	}
}
