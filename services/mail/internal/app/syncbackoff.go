package app

import (
	"sync"
	"time"
)

// What happens when the mailboxes that fail are the ones that hold the workers.
//
// A bounded fleet stops one broken mailbox from delaying every other one, but
// it does not stop broken mailboxes from *accumulating* in the fleet, and they
// do — because failure is sticky in a way success is not. A healthy mailbox
// finishes in a fraction of a second and gives its worker back. A mailbox
// whose host accepts the connection and then says nothing holds its worker for
// the whole timeout, fails, and is back in the queue two minutes later to do
// it again. Fifteen broken mailboxes out of three hundred is an ordinary
// number, and they are in the fleet far more of the time than their share.
//
// Two rules answer that, and they answer different halves of it.
//
// Backoff answers "how often": a mailbox that just failed is not tried again
// on the next tick. Same shape as the send worker's retry ladder, and for the
// same reason — an authorisation that expired at nine will not have unexpired
// itself at nine-oh-two, so asking every two minutes for the rest of the week
// buys nothing and costs a worker every time.
//
// A reserved share answers "how many at worst": mailboxes with a failure
// behind them may hold only part of the fleet, so healthy mail keeps flowing
// even in the case backoff makes unlikely but cannot make impossible — enough
// broken mailboxes coming due at the same moment to fill every worker. Without
// it the guarantee is statistical; with it a person whose mailbox works is
// never waiting on a queue of mailboxes that do not.

// syncBackoff is how long a mailbox waits after consecutive failures.
//
// Short at first, because most failures are a blip and the mail behind them is
// somebody's work. Long at the end, because by the fifth failure this is not a
// blip, it is a mailbox that needs a person — and the surfaced error on the
// settings page is what gets it one, not a retry.
var syncBackoff = []time.Duration{
	0,               // first failure: try again next tick
	2 * time.Minute, // then skip a tick
	10 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
}

// failingShare is the fraction of the fleet mailboxes with a failure behind
// them may occupy. A quarter: enough that a transient outage across several
// mailboxes still gets retried promptly, little enough that three quarters of
// the workers are always available to mail that is arriving normally.
const failingShare = 4

type mailboxHealth struct {
	failures int
	nextTry  time.Time
}

type syncHealth struct {
	mu sync.Mutex
	by map[int64]*mailboxHealth
	// Slots for mailboxes that are known to be failing. Separate from the
	// fleet's own semaphore, and smaller.
	sick chan struct{}
}

func newSyncHealth(concurrency int) *syncHealth {
	sick := concurrency / failingShare
	if sick < 1 {
		sick = 1
	}
	return &syncHealth{by: map[int64]*mailboxHealth{}, sick: make(chan struct{}, sick)}
}

// dueAt reports whether this mailbox should be attempted now, and whether it
// counts as a failing one.
func (h *syncHealth) dueAt(mailbox int64, now time.Time) (due, failing bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m, known := h.by[mailbox]
	if !known || m.failures == 0 {
		return true, false
	}
	return !now.Before(m.nextTry), true
}

// record updates a mailbox's health after an attempt.
func (h *syncHealth) record(mailbox int64, now time.Time, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err == nil {
		// Recovered. Forgotten entirely rather than decayed: a mailbox that
		// works is not on probation for its past.
		delete(h.by, mailbox)
		return
	}
	m, known := h.by[mailbox]
	if !known {
		m = &mailboxHealth{}
		h.by[mailbox] = m
	}
	m.failures++
	step := m.failures - 1
	if step >= len(syncBackoff) {
		step = len(syncBackoff) - 1
	}
	m.nextTry = now.Add(syncBackoff[step])
}

// holdSick claims one of the reserved slots for a failing mailbox, or reports
// that they are all taken and this attempt should wait for the next tick.
//
// Non-blocking on purpose. A failing mailbox that queued for a slot would be
// holding a fleet worker while it waited, which is the thing being prevented.
func (h *syncHealth) holdSick() (release func(), ok bool) {
	select {
	case h.sick <- struct{}{}:
		return func() { <-h.sick }, true
	default:
		return nil, false
	}
}

// failingCount is how many mailboxes are currently in a failed state.
func (h *syncHealth) failingCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, m := range h.by {
		if m.failures > 0 {
			n++
		}
	}
	return n
}
