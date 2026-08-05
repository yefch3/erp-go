package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The poller used to walk its mailboxes one at a time, so a mailbox whose host
// accepted the connection and then said nothing held up every mailbox behind
// it — for the whole dial timeout, every cycle, until somebody noticed. These
// tests pin the two properties that replace that: a bounded number of
// mailboxes in flight, and never the same mailbox twice at once.

var errSync = errors.New("host said no")

func TestFleetRunsMailboxesTogetherButNotAllAtOnce(t *testing.T) {
	const workers, mailboxes = 4, 40
	f := newSyncFleet(workers)

	var mu sync.Mutex
	inFlight, peak := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < mailboxes; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			_, _ = f.do(context.Background(), id, func() (int, error) {
				mu.Lock()
				inFlight++
				if inFlight > peak {
					peak = inFlight
				}
				mu.Unlock()
				time.Sleep(2 * time.Millisecond)
				mu.Lock()
				inFlight--
				mu.Unlock()
				return 0, nil
			})
		}(int64(i))
	}
	wg.Wait()

	if peak > workers {
		t.Errorf("%d mailboxes synced at once, cap is %d\n"+
			"the cap is not about our resources — it is what one client may ask of a mail host at a time",
			peak, workers)
	}
	if peak < 2 {
		t.Errorf("peak concurrency was %d: the fleet is still walking them one at a time", peak)
	}
}

func TestFleetSyncsOneMailboxOnceEvenWhenAskedTwice(t *testing.T) {
	f := newSyncFleet(8)
	var runs atomic.Int32
	release := make(chan struct{})

	// Somebody clicks 立即收信 while the poller is already inside their
	// mailbox. Two passes over the same UIDs would double the work and race
	// the sync state's high-water mark against itself.
	var wg sync.WaitGroup
	results := make([]int, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			n, _ := f.do(context.Background(), 7, func() (int, error) {
				runs.Add(1)
				<-release
				return 42, nil
			})
			results[slot] = n
		}(i)
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := runs.Load(); got != 1 {
		t.Errorf("the mailbox was synced %d times, want 1", got)
	}
	// Everybody who waited gets the answer, rather than an empty result.
	for i, n := range results {
		if n != 42 {
			t.Errorf("caller %d got %d, want the running sync's result 42", i, n)
		}
	}
}

func TestFleetReleasesAMailboxAfterItFails(t *testing.T) {
	f := newSyncFleet(2)
	boom := errors.New("host said no")

	if _, err := f.do(context.Background(), 1, func() (int, error) { return 0, boom }); err != boom {
		t.Fatalf("got %v, want the sync's own error", err)
	}
	// A failed sync must not leave the mailbox claimed: it fails every cycle
	// while somebody's authorisation is expired, and a claim that outlived the
	// failure would mean that mailbox never syncs again even once fixed.
	done := make(chan struct{})
	go func() {
		_, _ = f.do(context.Background(), 1, func() (int, error) { return 1, nil })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the mailbox stayed claimed after its sync failed")
	}
	if f.running() != 0 {
		t.Errorf("%d mailboxes still marked in flight after everything finished", f.running())
	}
}

func TestFleetDoesNotStrandAWaiterWhoseCallerGaveUp(t *testing.T) {
	f := newSyncFleet(1)
	release := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_, _ = f.do(context.Background(), 3, func() (int, error) {
			close(started)
			<-release
			return 0, nil
		})
	}()
	<-started

	// A second request for the same mailbox, whose caller then goes away —
	// a browser tab closed mid-request. It must return, not block for ever.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.do(ctx, 3, func() (int, error) { return 0, nil })
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a cancelled caller was reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("a cancelled caller blocked on somebody else's sync")
	}
	close(release)
}

// The question a bounded fleet does not answer on its own: what if every
// worker is held by a mailbox that is stuck?
//
// It is not hypothetical. Failure is sticky in a way success is not — a
// healthy mailbox finishes in a fraction of a second and hands its worker
// back, while a broken one holds it for the whole timeout and is queued again
// two minutes later. Left alone, the fleet silts up with the mailboxes that
// cannot use it.

func TestBrokenMailboxesStopAskingSoOften(t *testing.T) {
	h := newSyncHealth(8)
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)

	// A mailbox nobody has heard of is always due.
	if due, failing := h.dueAt(1, now); !due || failing {
		t.Fatalf("an unknown mailbox: due=%v failing=%v, want true/false", due, failing)
	}

	// First failure still retries promptly: most failures are a blip and the
	// mail behind them is somebody's work.
	h.record(1, now, errSync)
	if due, failing := h.dueAt(1, now); !due || !failing {
		t.Errorf("after one failure: due=%v failing=%v, want a prompt retry that counts as failing", due, failing)
	}

	// Keeping at it earns longer and longer waits.
	h.record(1, now, errSync)
	if due, _ := h.dueAt(1, now.Add(time.Minute)); due {
		t.Error("a mailbox that failed twice was retried a minute later; an expired authorisation does not unexpire itself")
	}
	if due, _ := h.dueAt(1, now.Add(3*time.Minute)); !due {
		t.Error("the second-failure wait outlasted its own backoff step")
	}

	// And a mailbox that recovers is forgiven completely rather than left on
	// probation for its past.
	h.record(1, now, nil)
	if due, failing := h.dueAt(1, now); !due || failing {
		t.Errorf("after recovering: due=%v failing=%v, want treated as healthy again", due, failing)
	}
}

func TestBrokenMailboxesCannotFillTheFleet(t *testing.T) {
	const workers = 8
	h := newSyncHealth(workers)

	// Every failing mailbox coming due at the same moment is the case backoff
	// makes unlikely and cannot make impossible. Only the reserved share may
	// be held, so somebody whose mailbox works is never queued behind a crowd
	// of mailboxes that do not.
	held := 0
	var releases []func()
	for i := 0; i < workers*3; i++ {
		if release, ok := h.holdSick(); ok {
			held++
			releases = append(releases, release)
		}
	}
	want := workers / failingShare
	if held != want {
		t.Fatalf("%d failing mailboxes got into the fleet at once, the reserved share is %d\n"+
			"the rest of the fleet must stay available to mail that is arriving normally", held, want)
	}
	if held >= workers {
		t.Fatal("failing mailboxes could fill every worker")
	}

	// Slots come back, or a transient outage would lock them out for ever.
	releases[0]()
	if _, ok := h.holdSick(); !ok {
		t.Error("a released slot was not reusable")
	}
}
