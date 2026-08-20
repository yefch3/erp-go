package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// 立即收信 answers on the inbox, not on the whole pass.
//
// A full pass is six folder-level round trips: INBOX, SENT and JUNK synced,
// then read state reconciled back over all three. Five sixths of that is work
// the person did not ask for - they clicked to see whether the customer had
// replied, and the reply is in the first sixth. These tests pin that the
// answer comes early, that the rest still happens, and that the two failure
// shapes which would otherwise hang the caller do not.

// fakePass stands in for syncMailboxNow: it signals when the inbox leg is
// done and then keeps working for as long as the tail takes.
func fakePass(inboxAfter, tailFor time.Duration, n int, inboxErr error, tailRan *atomic.Bool) func(chan<- syncOutcome) (int, error) {
	return func(done chan<- syncOutcome) (int, error) {
		time.Sleep(inboxAfter)
		if inboxErr != nil {
			return 0, inboxErr
		}
		select {
		case done <- syncOutcome{n, nil}:
		default:
		}
		time.Sleep(tailFor)
		tailRan.Store(true)
		return n, nil
	}
}

// runInteractive drives SyncMailboxInteractive's control flow over a stub
// pass, so the wiring is under test without a mail host behind it.
//
// The waiting half is not reimplemented here — it calls awaitInbox, the same
// function production calls. A test that reimplements the logic it is meant to
// pin will agree with itself forever, including about a bug.
func runInteractive(ctx context.Context, f *syncFleet, mailbox int64,
	pass func(chan<- syncOutcome) (int, error)) (int, error) {

	n, _, err := runInteractiveWithin(ctx, f, mailbox, interactiveWait, pass)
	return n, err
}

func runInteractiveWithin(ctx context.Context, f *syncFleet, mailbox int64,
	wait time.Duration, pass func(chan<- syncOutcome) (int, error)) (int, bool, error) {

	inbox := make(chan syncOutcome, 1)
	tail, cancel := context.WithTimeout(context.WithoutCancel(ctx), inboxTail)
	go func() {
		defer cancel()
		n, err := f.do(tail, mailbox, func() (int, error) { return pass(inbox) })
		select {
		case inbox <- syncOutcome{n, err}:
		default:
		}
	}()
	return awaitInbox(ctx, inbox, wait)
}

func TestInteractiveSyncAnswersOnTheInboxNotTheWholePass(t *testing.T) {
	var tailRan atomic.Bool
	f := newSyncFleet(8)

	start := time.Now()
	n, err := runInteractive(context.Background(), f, 1,
		fakePass(20*time.Millisecond, 400*time.Millisecond, 3, nil, &tailRan))
	waited := time.Since(start)

	if err != nil {
		t.Fatalf("interactive sync returned %v", err)
	}
	if n != 3 {
		t.Errorf("reported %d new messages, want 3", n)
	}
	if waited > 200*time.Millisecond {
		t.Errorf("caller waited %v: that is the whole pass, not the inbox leg", waited)
	}
	if tailRan.Load() {
		t.Error("the tail had already finished; this test is not measuring what it claims")
	}

	// And the work nobody waited for still happens.
	deadline := time.After(3 * time.Second)
	for !tailRan.Load() {
		select {
		case <-deadline:
			t.Fatal("the sent/junk/reconcile tail never ran after the caller was answered")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// The failure that would hang: when the inbox leg errors, nothing signals the
// channel. Without the fallback send after fleet.do returns, the caller waits
// on a channel nobody will ever write to, and 立即收信 spins until the request
// times out instead of reporting the error it already has.
func TestInteractiveSyncReportsAnInboxFailureRatherThanHanging(t *testing.T) {
	boom := errors.New("authorisation expired")
	var tailRan atomic.Bool
	f := newSyncFleet(8)

	done := make(chan error, 1)
	go func() {
		_, err := runInteractive(context.Background(), f, 1,
			fakePass(10*time.Millisecond, 0, 0, boom, &tailRan))
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, boom) {
			t.Errorf("returned %v, want the inbox failure", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("interactive sync hung on a failed inbox leg")
	}
}

// The other way nobody signals: the fleet joins this caller onto a pass that
// is already running for the same mailbox, so this goroutine's stub never
// executes and never writes to this caller's channel.
func TestInteractiveSyncAnswersWhenJoinedOntoARunningPass(t *testing.T) {
	f := newSyncFleet(8)
	var firstRan, secondRan atomic.Bool

	// Occupy the mailbox with a slow pass that does not know about the
	// second caller's channel.
	go func() {
		_, _ = f.do(context.Background(), 42, func() (int, error) {
			firstRan.Store(true)
			time.Sleep(150 * time.Millisecond)
			return 7, nil
		})
	}()
	for !firstRan.Load() {
		time.Sleep(time.Millisecond)
	}

	done := make(chan int, 1)
	go func() {
		n, err := runInteractive(context.Background(), f, 42,
			func(chan<- syncOutcome) (int, error) {
				secondRan.Store(true)
				return 99, nil
			})
		if err != nil {
			t.Errorf("joined caller returned %v", err)
		}
		done <- n
	}()

	select {
	case n := <-done:
		if secondRan.Load() {
			t.Error("the second caller ran its own pass; the fleet should have joined it onto the first")
		}
		if n != 7 {
			t.Errorf("joined caller got %d, want the running pass's result 7", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("interactive sync hung when joined onto a pass already in flight")
	}
}

// Navigating away must not abandon the pass: the mail is worth having whether
// or not the person who asked for it is still looking.
func TestInteractiveSyncTailSurvivesTheCallerGivingUp(t *testing.T) {
	var tailRan atomic.Bool
	f := newSyncFleet(8)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := runInteractive(ctx, f, 1,
		fakePass(200*time.Millisecond, 50*time.Millisecond, 1, nil, &tailRan))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("caller returned %v, want context.Canceled", err)
	}

	deadline := time.After(3 * time.Second)
	for !tailRan.Load() {
		select {
		case <-deadline:
			t.Fatal("the pass was abandoned when the caller went away")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// 一个从没同步过的邮箱，收件箱这一程可能要几分钟。请求不能一直挂着：调用方前面
// 是 nginx，60 秒不响应就是 504 —— 一个页面报错，报的却是一件正在正常进行的事。
//
// 到点要给出的答案是"还在收"，而不是错误，也不是假装收完了。
func TestInteractiveSyncAnswersStillRunningRatherThanHanging(t *testing.T) {
	f := newSyncFleet(1)
	var tailRan atomic.Bool
	// 收件箱那一程比等待窗口长得多。
	pass := fakePass(300*time.Millisecond, 10*time.Millisecond, 7, nil, &tailRan)

	start := time.Now()
	n, pending, err := runInteractiveWithin(context.Background(), f, 1, 30*time.Millisecond, pass)
	waited := time.Since(start)

	if err != nil {
		t.Fatalf("还在收不是错误，却返回了 %v", err)
	}
	if !pending {
		t.Error("pending = false，期望 true")
	}
	if n != 0 {
		t.Errorf("fetched = %d，期望 0 —— 还没数出来的数字不该报给用户", n)
	}
	if waited > 200*time.Millisecond {
		t.Errorf("等了 %v，远超给定的 30ms 窗口", waited)
	}

	// 尾巴照跑：人不等了，信还是要收进来的。
	deadline := time.Now().Add(2 * time.Second)
	for !tailRan.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !tailRan.Load() {
		t.Error("调用方拿到「还在收」之后，后台那一程没有继续")
	}
}

// 收得快的时候不该白等满一个窗口。
func TestInteractiveSyncStillAnswersImmediatelyWhenTheInboxIsQuick(t *testing.T) {
	f := newSyncFleet(1)
	var tailRan atomic.Bool
	pass := fakePass(time.Millisecond, time.Millisecond, 3, nil, &tailRan)

	start := time.Now()
	n, pending, err := runInteractiveWithin(context.Background(), f, 1, time.Second, pass)

	if err != nil {
		t.Fatal(err)
	}
	if pending {
		t.Error("pending = true，但收件箱已经收完了")
	}
	if n != 3 {
		t.Errorf("fetched = %d，期望 3", n)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("等了 %v —— 收完就该立刻返回，不是等满窗口", d)
	}
}
