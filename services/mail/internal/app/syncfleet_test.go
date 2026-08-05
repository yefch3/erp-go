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
