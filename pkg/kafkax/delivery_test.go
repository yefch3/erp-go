package kafkax

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/segmentio/kafka-go"

	"github.com/sgao19/erp-go/pkg/idempotency"
)

// 事件既不能丢，也不能记两遍。这组测试守的是这两条。
//
// 走过两版历史：
//   一版：认领先单独提交、失败不撤销 → handler 失败一次，事件永久消失，
//         而日志写着 will retry。一次数据库抖动吃掉一笔收货。
//   二版：认领仍在前面但失败时归还 → 常见故障不丢了，硬杀还是丢：认领已
//         提交、活没提交，重投时跳过一件从没干完的事。
//   现在：认领写在 handler 自己的事务里，两者同生共死。
//
// 「进程被 kill -9」在单元测试里没法真的发生，所以这里用 fakeTx 精确复刻它
// 的语义：事务没提交 = 里面的写入（含认领）一概不算数。

// fakeTx 只需要满足 pgx.Tx 的形状；测试只用到「提交与否」这一件事。
type fakeTx struct {
	pgx.Tx
	claims    []string
	committed bool
}

// txDeduper 用内存复刻 processed_events 的行为：认领先写进事务的暂存区，
// 事务提交时才落到「库」里——没提交就等于没发生。
type txDeduper struct {
	mu        sync.Mutex
	committed map[string]bool // 已提交的认领
	probes    int
}

func newTxDeduper() *txDeduper { return &txDeduper{committed: map[string]bool{}} }

func (d *txDeduper) AlreadyProcessed(_ context.Context, key string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.probes++
	return d.committed[key], nil
}

func (d *txDeduper) ClaimInTx(_ context.Context, tx pgx.Tx, key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.committed[key] {
		return idempotency.ErrAlreadyClaimed
	}
	// 记在这个事务上；只有 commit() 被调用才算数。
	ft, _ := tx.(*fakeTx)
	if ft == nil {
		return errors.New("claim called outside a transaction")
	}
	ft.claims = append(ft.claims, key)
	return nil
}

// commit 复刻事务提交：认领此刻才对外可见。
func (d *txDeduper) commit(ft *fakeTx) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, k := range ft.claims {
		d.committed[k] = true
	}
	ft.committed = true
}

func (d *txDeduper) isClaimed(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.committed[key]
}

type parkedEvent struct {
	key    string
	reason string
	tenant int64
}

type fakeDeadLetter struct {
	mu     sync.Mutex
	parked []parkedEvent
	err    error
}

func (p *fakeDeadLetter) Park(_ context.Context, tenantID int64, key, _, _, _ string, _ []byte, reason string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.parked = append(p.parked, parkedEvent{key: key, reason: reason, tenant: tenantID})
	return nil
}

func (p *fakeDeadLetter) all() []parkedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]parkedEvent(nil), p.parked...)
}

func newTestConsumer(r reader, d Deduper, h Handler) *Consumer {
	return &Consumer{
		r: r, topic: "t", group: "g", dedupe: d, handler: h,
		log:   discardLogger(),
		sleep: func(context.Context, time.Duration) error { return nil },
	}
}

func envelopeMsg(eventID int64) kafka.Message {
	v, _ := json.Marshal(Envelope{
		EventID: eventID, TenantID: 2, AggregateType: "purchase",
		EventType: "purchase.received", AggregateID: "PO-1",
		Payload: json.RawMessage(`{"receipt_no":"R-1"}`),
	})
	return kafka.Message{Topic: "t", Value: v}
}

func envelopeOf(t *testing.T, id int64) Envelope {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(envelopeMsg(id).Value, &env); err != nil {
		t.Fatal(err)
	}
	return env
}

// 正常路径：handler 在事务里认领并干活，提交，偏移量可以提交。
func TestSuccessCommitsClaimWithTheWork(t *testing.T) {
	d := newTxDeduper()
	var worked int
	c := newTestConsumer(nil, d, func(ctx context.Context, e Envelope, claim Claim) error {
		tx := &fakeTx{}
		if err := claim(ctx, tx); err != nil {
			return err
		}
		worked++
		d.commit(tx) // 业务事务提交
		return nil
	})

	env := envelopeOf(t, 1)
	if ok := c.handleOne(context.Background(), env); !ok {
		t.Fatal("处理成功了却不让提交偏移量")
	}
	if worked != 1 || !d.isClaimed(env.DedupeKey()) {
		t.Fatalf("干活 %d 次，认领=%v——两者必须同时发生", worked, d.isClaimed(env.DedupeKey()))
	}
	// 第二次投递（偏移量提交失败会发生）：跳过，不重复记账。
	if ok := c.handleOne(context.Background(), env); !ok {
		t.Fatal("重投时该跳过并提交偏移量")
	}
	if worked != 1 {
		t.Fatalf("同一条事件干了 %d 次——库存会翻倍", worked)
	}
}

// **F1 的核心**：事务提交前被硬杀，认领随事务一起消失，重投时从头再来。
//
// 旧实现在这里会丢：认领早就单独提交了，重投时看到它就跳过，而活从没干完。
func TestAHardKillBeforeCommitLosesNothing(t *testing.T) {
	d := newTxDeduper()
	var attempts, completed int
	c := newTestConsumer(nil, d, func(ctx context.Context, e Envelope, claim Claim) error {
		attempts++
		tx := &fakeTx{}
		if err := claim(ctx, tx); err != nil {
			return err
		}
		if attempts == 1 {
			// 认领已写进事务，业务也做了一半——此刻进程被 kill -9。
			// 事务没提交，所以这一切都不算数。
			return errors.New("process killed mid-transaction")
		}
		completed++
		d.commit(tx)
		return nil
	})

	env := envelopeOf(t, 7)
	if ok := c.handleOne(context.Background(), env); !ok {
		t.Fatal("重试之后成功了，该提交偏移量")
	}
	if completed != 1 {
		t.Fatalf("这笔活最终该干成一次，实际 %d 次", completed)
	}
	if !d.isClaimed(env.DedupeKey()) {
		t.Fatal("干成了却没留下认领——下次重投会再干一遍")
	}
}

// 被杀之后**换一个进程**重投：同样必须从头再来，而不是被当成干过了。
func TestARedeliveryAfterAKillRedoesTheWork(t *testing.T) {
	d := newTxDeduper()
	env := envelopeOf(t, 9)

	// 第一个进程：认领进事务，然后死掉（事务从未提交）。
	killed := newTestConsumer(nil, d, func(ctx context.Context, e Envelope, claim Claim) error {
		tx := &fakeTx{}
		_ = claim(ctx, tx)
		return errors.New("killed")
	})
	killed.dead = &fakeDeadLetter{}
	_ = killed.handleOne(context.Background(), env)

	if d.isClaimed(env.DedupeKey()) {
		t.Fatal("事务没提交，认领却留下了——这正是硬杀丢事件的根源")
	}

	// 第二个进程接手：必须真的把活干了。
	var worked int
	fresh := newTestConsumer(nil, d, func(ctx context.Context, e Envelope, claim Claim) error {
		tx := &fakeTx{}
		if err := claim(ctx, tx); err != nil {
			return err
		}
		worked++
		d.commit(tx)
		return nil
	})
	if ok := fresh.handleOne(context.Background(), env); !ok {
		t.Fatal("接手的进程该把这条事件处理掉")
	}
	if worked != 1 {
		t.Fatalf("接手之后该干成一次，实际 %d 次——事件丢了", worked)
	}
}

// 并发：两个消费者同时处理同一条，只有一个能认领成功，另一个安全退出。
func TestConcurrentClaimLetsExactlyOneThrough(t *testing.T) {
	d := newTxDeduper()
	env := envelopeOf(t, 11)

	// 甲先干完并提交。
	first := newTestConsumer(nil, d, func(ctx context.Context, e Envelope, claim Claim) error {
		tx := &fakeTx{}
		if err := claim(ctx, tx); err != nil {
			return err
		}
		d.commit(tx)
		return nil
	})
	if ok := first.handleOne(context.Background(), env); !ok {
		t.Fatal("甲该处理成功")
	}

	// 乙在甲提交之后才走到认领这一步：拿到 ErrAlreadyClaimed，事务回滚，
	// 且不该被当成失败去重试或进死信。
	var worked int
	second := newTestConsumer(nil, d, func(ctx context.Context, e Envelope, claim Claim) error {
		tx := &fakeTx{}
		if err := claim(ctx, tx); err != nil {
			return err // 业务代码把它原样抛出，事务回滚
		}
		worked++
		d.commit(tx)
		return nil
	})
	dead := &fakeDeadLetter{}
	second.dead = dead
	if ok := second.handleOne(context.Background(), env); !ok {
		t.Fatal("活已经被甲干完了，乙该提交偏移量往前走")
	}
	if worked != 0 {
		t.Fatal("乙又干了一遍——库存翻倍")
	}
	if len(dead.all()) != 0 {
		t.Fatal("这不是失败，不该进死信表")
	}
}

// 跳过不相关的事件类型：不认领、不报错，偏移量照常提交。
func TestSkippedEventNeedsNoClaim(t *testing.T) {
	d := newTxDeduper()
	c := newTestConsumer(nil, d, func(context.Context, Envelope, Claim) error {
		return nil // 事件类型不是我的，直接跳过
	})
	env := envelopeOf(t, 13)
	if ok := c.handleOne(context.Background(), env); !ok {
		t.Fatal("跳过的事件该提交偏移量，否则它会一直挡在队首")
	}
	if d.isClaimed(env.DedupeKey()) {
		t.Fatal("跳过的事件不该留下认领——它根本没有事务")
	}
}

// 处理不了的事件进死信表，队列放行。
func TestAnUnprocessableEventIsParkedAndTheQueueMovesOn(t *testing.T) {
	d := newTxDeduper()
	dead := &fakeDeadLetter{}
	c := newTestConsumer(nil, d, func(context.Context, Envelope, Claim) error {
		return errors.New("IV_NO_WAREHOUSE: 没有可用仓库，无法收货")
	})
	c.dead = dead

	env := envelopeOf(t, 15)
	if ok := c.handleOne(context.Background(), env); !ok {
		t.Fatal("已经停进死信表了，就该提交偏移量让队列继续走")
	}
	parked := dead.all()
	if len(parked) != 1 || parked[0].tenant != 2 || parked[0].reason == "" {
		t.Fatalf("死信要记得是哪家公司、为什么失败：%+v", parked)
	}
	if d.isClaimed(env.DedupeKey()) {
		t.Fatal("没干成的事件不该留下认领——重放时它必须能重新跑一遍")
	}
}

// 停不进死信表时绝不提交：既没干成、又没记下来的事件正是这一切要防的。
func TestAnEventIsNotCommittedWhenParkingFails(t *testing.T) {
	d := newTxDeduper()
	c := newTestConsumer(nil, d, func(context.Context, Envelope, Claim) error {
		return errors.New("boom")
	})
	c.dead = &fakeDeadLetter{err: errors.New("database is down")}
	if ok := c.handleOne(context.Background(), envelopeOf(t, 17)); ok {
		t.Fatal("既没处理成功、也没记进死信表，却提交了偏移量——事件凭空消失")
	}
}

// 没配死信表时宁可堵住也不丢。
func TestWithoutADeadLetterTheEventIsKeptRatherThanDropped(t *testing.T) {
	d := newTxDeduper()
	c := newTestConsumer(nil, d, func(context.Context, Envelope, Claim) error {
		return errors.New("boom")
	})
	if ok := c.handleOne(context.Background(), envelopeOf(t, 19)); ok {
		t.Fatal("没地方停放，却把事件提交掉了——这就是悄悄丢失")
	}
}

// 去重表本身查不了的时候不提交，交给 Run 的循环去退避。
func TestADedupeFailureDoesNotCommit(t *testing.T) {
	c := newTestConsumer(nil, failingDeduper{}, func(context.Context, Envelope, Claim) error {
		return nil
	})
	if ok := c.handleOne(context.Background(), envelopeOf(t, 21)); ok {
		t.Fatal("查都没查到，不该提交偏移量")
	}
}

type failingDeduper struct{}

func (failingDeduper) AlreadyProcessed(context.Context, string) (bool, error) {
	return false, errors.New("database is down")
}
func (failingDeduper) ClaimInTx(context.Context, pgx.Tx, string) error { return nil }

// 退避是涨的：1、2、4、8 秒，且不超过上限。
func TestBackoffGrowsAndIsCapped(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	for i, w := range want {
		if got := backoffFor(i + 1); got != w {
			t.Fatalf("第 %d 次重试该等 %v，实际 %v", i+1, w, got)
		}
	}
	if got := backoffFor(20); got != fetchRetryMax {
		t.Fatalf("退避该封顶在 %v，实际 %v", fetchRetryMax, got)
	}
}
