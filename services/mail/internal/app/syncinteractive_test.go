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

// runInteractive mirrors SyncMailboxInteractive's control flow over a stub
// pass, so the wiring is under test without a mail host behind it.
func runInteractive(ctx context.Context, f *syncFleet, mailbox int64,
	pass func(chan<- syncOutcome) (int, error)) (int, error) {

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
	select {
	case r := <-inbox:
		return r.n, r.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
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
