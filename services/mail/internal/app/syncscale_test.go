package app

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"
)

// What a cycle costs at company scale.
//
// Every property the fleet guarantees was pinned at 40 mailboxes, where a
// cycle finishes in milliseconds whatever the scheduler does. The question
// these tests answer is the one that only appears at 300: does a pass over
// every mailbox still fit inside the interval it is supposed to repeat on?
//
// Nothing here talks to a mail host. The timings below stand in for one, and
// they are the input the conclusion depends on - if real mailboxes are slower
// than this, the measured cycle scales with them.

// Observed shape of an INBOX poll with nothing new in it: a pooled connection,
// SELECT, SEARCH, no FETCH. The tail is what a cold TLS handshake costs, or a
// mailbox with a few new messages to pull down.
var syncLatency = struct{ p50, p90, p99 time.Duration }{
	p50: 8 * time.Millisecond,
	p90: 30 * time.Millisecond,
	p99: 100 * time.Millisecond,
}

// scaleFactor converts the milliseconds above into the seconds a real mail
// host takes. Kept out of the sleep so the test runs in about a second and
// the arithmetic stays visible: 8ms here stands for 0.8s in production.
const scaleFactor = 100

func latencyFor(i int) time.Duration {
	switch {
	case i%100 == 0:
		return syncLatency.p99
	case i%10 == 0:
		return syncLatency.p90
	default:
		return syncLatency.p50
	}
}

// oneCycle runs a full pass the way syncAllMailboxes does - a goroutine per
// mailbox, all of them queueing on the fleet - and reports how long the pass
// took and how long each mailbox waited from tick to its own turn.
func oneCycle(t *testing.T, workers, mailboxes int, due func(int) bool) (cycle time.Duration, waits []time.Duration, synced int) {
	t.Helper()
	f := newSyncFleet(workers)
	start := time.Now()

	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < mailboxes; i++ {
		if !due(i) {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = f.do(context.Background(), int64(i), func() (int, error) {
				mu.Lock()
				waits = append(waits, time.Since(start))
				synced++
				mu.Unlock()
				time.Sleep(latencyFor(i))
				return 0, nil
			})
		}(i)
	}
	wg.Wait()
	return time.Since(start), waits, synced
}

func percentile(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	i := int(float64(len(s)-1) * p)
	return s[i]
}

// scaled reports a measured duration as the production figure it stands for.
func scaled(d time.Duration) time.Duration {
	return (d * scaleFactor).Round(time.Second)
}

// A cycle is mailboxes x latency / workers, and the useful output is not one
// number but the latency at which that exceeds the interval. Below the
// break-even the poller keeps its promise; above it, ticks start landing on a
// pass that has not finished, and "you see a reply within two minutes"
// silently becomes something longer that nobody measured.
func breakEven(mailboxes, workers int, interval time.Duration) time.Duration {
	return interval * time.Duration(workers) / time.Duration(mailboxes)
}

// At the shipped settings the margin is thinner than it looks. 300 mailboxes
// on 8 workers tolerates about 3.2s per mailbox - fine for a warm pooled
// connection, not obviously fine for a cross-Pacific hop to a provider having
// a slow morning. This test records where the edge is so that a change to the
// fleet size, the interval or the customer's size has to move it on purpose.
func TestCycleTimeAtCompanyScale(t *testing.T) {
	const interval = 2 * time.Minute

	for _, tc := range []struct {
		name      string
		workers   int
		mailboxes int
	}{
		{"pilot: 20 mailboxes, shipped concurrency", 8, 20},
		{"department: 80 mailboxes, shipped concurrency", 8, 80},
		{"company: 300 mailboxes, shipped concurrency", 8, 300},
		{"company: 300 mailboxes, concurrency 24", 24, 300},
		{"company: 300 mailboxes, concurrency 48", 48, 300},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cycle, waits, synced := oneCycle(t, tc.workers, tc.mailboxes, func(int) bool { return true })
			got, p95 := scaled(cycle), scaled(percentile(waits, 0.95))
			edge := breakEven(tc.mailboxes, tc.workers, interval)

			t.Logf("%3d mailboxes / %2d workers: cycle %v, p95 wait for a turn %v, "+
				"overruns once a mailbox averages %v",
				tc.mailboxes, tc.workers, got, p95, edge.Round(100*time.Millisecond))

			if synced != tc.mailboxes {
				t.Errorf("synced %d of %d mailboxes", synced, tc.mailboxes)
			}
			// Deliberately loose. The measured cycle is here to be read, not
			// to gate the build - it moves with whatever else the machine is
			// running, and a test that fails because the suite got busier
			// teaches nobody anything. Four times the interval means the
			// scheduler is broken, not that the box is busy.
			if got > 4*interval {
				t.Errorf("cycle %v is %vx the interval: not load, a regression", got, got/interval)
			}
		})
	}
}

// The margin at the shipped settings, stated as the thing that would consume
// it. Anyone raising the mailbox count or lowering the fleet size has to come
// past this test, which is the point: 3.2s of headroom is a fact about this
// configuration, not a property of the design.
func TestShippedSettingsLeaveLittleHeadroomAtCompanyScale(t *testing.T) {
	const (
		interval  = 2 * time.Minute
		mailboxes = 300
		workers   = 8
	)
	edge := breakEven(mailboxes, workers, interval)

	t.Logf("at %d mailboxes on %d workers the cycle overruns once the average "+
		"mailbox takes %v", mailboxes, workers, edge.Round(100*time.Millisecond))

	if edge > 5*time.Second {
		t.Errorf("break-even is %v, which is more headroom than this configuration "+
			"used to have - if the fleet or interval changed, re-derive the tiers "+
			"rather than assuming they are still needed", edge)
	}
	if edge < 2*time.Second {
		t.Errorf("break-even is %v: an ordinary mailbox on an ordinary day would "+
			"overrun the interval. Raise MAIL_SYNC_CONCURRENCY or tier the "+
			"schedule before putting this many mailboxes on it", edge)
	}
}

// The fix, measured. Tiering by activity does not make a mailbox sync faster;
// it stops the poller from asking 300 of them when only a fraction are being
// read. The mailboxes that someone is actually watching get their turn sooner
// precisely because the idle ones are not in the queue ahead of them.
//
// Asserted on the work, logged on the clock. An earlier version of this test
// asserted that the tiered cycle finished at least 3x sooner, which is true
// and was still the wrong thing to check: wall-clock ratios move with whatever
// else the machine is doing, and it duly passed alone and failed inside the
// full suite. What tiering actually changes is how many mailboxes get asked,
// and that is exact.
func TestTieredSchedulingAsksForLessWork(t *testing.T) {
	const (
		mailboxes = 300
		workers   = 8
	)

	// A working day at a 300-person trading company: a minority have the
	// mailbox open right now, the rest are asleep, on the road, or belong to
	// the warehouse account nobody reads from.
	activeShare := func(i int) bool { return i%6 == 0 } // 50 of 300

	flat, flatWaits, flatN := oneCycle(t, workers, mailboxes, func(int) bool { return true })
	tiered, tieredWaits, tieredN := oneCycle(t, workers, mailboxes, activeShare)

	t.Logf("every mailbox every tick: %d synced, cycle %v, p95 wait %v",
		flatN, scaled(flat), scaled(percentile(flatWaits, 0.95)))
	t.Logf("active mailboxes only:    %d synced, cycle %v, p95 wait %v",
		tieredN, scaled(tiered), scaled(percentile(tieredWaits, 0.95)))

	if flatN != mailboxes {
		t.Fatalf("untiered pass synced %d of %d mailboxes", flatN, mailboxes)
	}
	if tieredN != 50 {
		t.Fatalf("tiered pass synced %d mailboxes, want the 50 active ones", tieredN)
	}
	// Six times less work asked of the mail host, which is the half of this
	// that raising MAIL_SYNC_CONCURRENCY cannot give: that spends the host's
	// tolerance to buy latency, this one spends neither.
	if ratio := flatN / tieredN; ratio != 6 {
		t.Errorf("tiering asked for 1/%d of the work, expected 1/6", ratio)
	}
}
