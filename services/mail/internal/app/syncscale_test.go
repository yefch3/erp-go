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

// 一人多箱之后，「每轮把每个箱都全量同步一遍」摊不开了。
//
// 这条测试把那个算术摆出来，并钉住分档之后的余量。数字都是 300 人的公司：
//
//	一人一箱  300 个箱  8 个 worker / 2 分钟  → 每个箱 3.2 秒   （原来就很紧）
//	一人两箱  600 个箱  同上                  → 每个箱 1.6 秒   （跨太平洋一次握手就超）
//
// 超了的样子不是报错，是所有人的信一起晚到，而且越积越晚——上一轮没跑完，
// 下一轮已经该开始了。
//
// 分档之后全量那一档只剩「有人在看的」那些，其余走一条 STATUS。下面两条
// 断言分别钉住这两档。
func TestTieringIsWhatMakesMultipleMailboxesFit(t *testing.T) {
	const (
		interval = 2 * time.Minute
		workers  = 8
		people   = 300
		perHead  = 2 // 一人两箱：公司箱 + 业务员自己的
	)
	all := people * perHead

	flat := breakEven(all, workers, interval)
	t.Logf("不分档：%d 个箱 / %d worker，每个箱只有 %v",
		all, workers, flat.Round(100*time.Millisecond))
	if flat >= 2*time.Second {
		t.Errorf("不分档的预算是 %v，比预期宽——如果 fleet 或轮询间隔改过了，"+
			"下面那两条的结论要跟着重算，而不是假定还成立", flat)
	}

	// 有人在看的那一档。一个 300 人的公司在上班时间大约 1/6 的人开着邮箱页，
	// 而一个人同一时刻只看一个箱——所以活跃箱数跟着**人**走，不跟着箱走。
	// 这是分档为什么有效的核心：分母不随人均箱数增长。
	const active = people / 6
	edge := breakEven(active, workers, interval)
	t.Logf("分档后全量这一档：%d 个箱 / %d worker，每个箱 %v",
		active, workers, edge.Round(100*time.Millisecond))
	if edge < 5*time.Second {
		t.Errorf("全量这一档每个箱只有 %v，仍然不够一次跨洋往返。"+
			"要么调大 MAIL_SYNC_CONCURRENCY，要么把 MAIL_SYNC_ACTIVE_WINDOW 收窄", edge)
	}

	// 轻状态那一档。没人看的箱每 MAIL_SYNC_STATUS_EVERY 问一条 STATUS，
	// 按每轮 MAIL_SYNC_STATUS_BUDGET 个摊开。
	const (
		statusEvery  = 10 * time.Minute
		statusBudget = 150
	)
	idle := all - active
	// 一遍要几轮，以及预算够不够把它们都轮到。
	rounds := int(statusEvery / interval)
	capacity := rounds * statusBudget
	t.Logf("轻状态这一档：%d 个箱，每 %v 一遍，%d 轮 × %d 个 = 容量 %d",
		idle, statusEvery, rounds, statusBudget, capacity)
	if capacity < idle {
		t.Errorf("每 %v 只轮得到 %d 个箱，而没人看的有 %d 个——"+
			"轮不到的那些收信延迟会超过 %v，而且是**哪些箱**不确定。"+
			"调大 MAIL_SYNC_STATUS_BUDGET 或放宽 MAIL_SYNC_STATUS_EVERY",
			statusEvery, capacity, idle, statusEvery)
	}
	// 轻状态占掉的 worker 时间要留得下全量那一档。一条 STATUS 按一次冷握手
	// 算（1.5 秒）——它不打开信箱、不拉正文，贵的只有连接本身。
	const statusCost = 1500 * time.Millisecond
	perRound := time.Duration(statusBudget) * statusCost / workers
	t.Logf("轻状态每轮占 %v / %v", perRound.Round(time.Second), interval)
	if perRound > interval/2 {
		t.Errorf("轻状态每轮就占掉 %v（一轮共 %v），全量那一档会被挤掉——"+
			"而被挤掉的正是有人正在等的那些", perRound, interval)
	}
}

// 常开连接那一档：一个信箱一条，所以它按**箱**数长，不按人数。
//
// 这是这套东西最贵的一项，也是 IDLE 必须跟着分档的理由。600 条一直占着的
// IMAP 连接，其中大部分箱当天根本没人打开过——而 IDLE 的用处是「让正在看的
// 那个收件箱像是活的」。
func TestIdleWatchersFollowThePersonNotTheMailbox(t *testing.T) {
	const (
		people  = 300
		perHead = 2
	)
	all := people * perHead
	const active = people / 6

	t.Logf("不分档：%d 条常开连接；分档后：%d 条", all, active)
	if active*4 > all {
		t.Errorf("分档后还剩 %d/%d 条常开连接，省得不够多——"+
			"如果活跃口径放宽了，先确认邮件服务商受得了这个并发", active, all)
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
