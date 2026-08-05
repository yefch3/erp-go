package app

import (
	"context"
	"sync"
)

// How many mailboxes sync at once, and how one mailbox is kept from syncing
// twice over.
//
// The poller walked its list one mailbox at a time. That is the wrong shape
// for work that is almost entirely waiting: a goroutine blocked on a socket
// costs nothing, so doing three hundred of these in sequence pays three
// hundred round trips where it could pay three hundred divided by the number
// of workers. Measured on a healthy mailbox with no new mail, a pass takes
// about two tenths of a second — three hundred of those is under a minute,
// which fits the two-minute tick, but that is the floor. A mailbox with fifty
// new messages downloads fifty bodies, and the tick has no give in it.
//
// The cap is not about our resources. A goroutine is a few kilobytes and a
// socket is cheap; three hundred of either would be unremarkable. It is about
// the host on the other end, which has opinions about how much one client may
// ask of it at once, and about failure: eight workers means a mailbox that
// hangs occupies an eighth of the fleet instead of all of it.

// syncFleet runs mailbox syncs with a bounded number in flight, and never the
// same mailbox twice at once.
type syncFleet struct {
	sem chan struct{}
	// Which mailboxes are failing, how long they wait, and how much of the
	// fleet they may hold. See syncbackoff.go.
	health *syncHealth

	mu       sync.Mutex
	inFlight map[int64]*syncCall
}

// syncCall is one mailbox's running sync, shared by everybody who asked for it
// while it was running.
type syncCall struct {
	done chan struct{}
	n    int
	err  error
}

func newSyncFleet(concurrency int) *syncFleet {
	if concurrency <= 0 {
		concurrency = 8
	}
	return &syncFleet{
		sem:      make(chan struct{}, concurrency),
		health:   newSyncHealth(concurrency),
		inFlight: map[int64]*syncCall{},
	}
}

// do runs fn for this mailbox unless one is already running, in which case it
// waits for that one and reports its result.
//
// Sharing rather than queueing, because two syncs of the same mailbox seconds
// apart are the same answer twice. Somebody clicking 立即收信 while the poller
// is already inside their mailbox should get the poller's result, not a second
// pass over the same UIDs — which would double the work, and race the sync
// state's high-water mark against itself.
func (f *syncFleet) do(ctx context.Context, mailbox int64, fn func() (int, error)) (int, error) {
	f.mu.Lock()
	if c, running := f.inFlight[mailbox]; running {
		f.mu.Unlock()
		select {
		case <-c.done:
			return c.n, c.err
		case <-ctx.Done():
			// The caller gave up. The sync carries on for whoever else is
			// waiting on it; abandoning it here would waste the work already
			// done and leave the mailbox half-synced.
			return 0, ctx.Err()
		}
	}
	c := &syncCall{done: make(chan struct{})}
	f.inFlight[mailbox] = c
	f.mu.Unlock()

	// The slot is taken after claiming the mailbox, not before: holding a
	// worker while waiting for a worker is how a pool deadlocks.
	select {
	case f.sem <- struct{}{}:
	case <-ctx.Done():
		f.finish(mailbox, c, 0, ctx.Err())
		return 0, ctx.Err()
	}
	n, err := fn()
	<-f.sem
	f.finish(mailbox, c, n, err)
	return n, err
}

func (f *syncFleet) finish(mailbox int64, c *syncCall, n int, err error) {
	f.mu.Lock()
	c.n, c.err = n, err
	delete(f.inFlight, mailbox)
	f.mu.Unlock()
	close(c.done)
}

// running reports how many mailboxes are syncing. For logging and tests.
func (f *syncFleet) running() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.inFlight)
}

// fleet is the service's one sync fleet, made on first use.
//
// Lazily rather than in the constructor because the concurrency comes from
// SyncConfig, which the background jobs carry and the constructor does not
// see. First caller wins; the value does not change at runtime.
func (s *Service) fleet(concurrency int) *syncFleet {
	s.fleetOnce.Do(func() { s.syncFleet = newSyncFleet(concurrency) })
	return s.syncFleet
}
