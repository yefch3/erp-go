package kafkax

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

// 一条事件失败一次就永远消失——这组测试守的是这件事。
//
// 原来的顺序是「先记下已处理，再去处理」。处理失败就不提交偏移量，等它重投；
// 重投回来一查「已处理」，于是**跳过处理直接提交**。一次失败，一笔采购收货
// 就没了，而日志写的是 will retry。谁都看不见：采购说收到了，库存没动。
//
// 认领仍然先记，那是故意的：这些 handler 一个都不是幂等的，同一笔到货记两遍
// 会把库存翻倍。所以认领要一直握着，只有在「活确实没干成」时才还回去。

// scriptedDeduper 记录认领与归还，好断言「失败之后认领被还回去了」。
type scriptedDeduper struct {
	mu       sync.Mutex
	seen     map[string]bool
	marks    int
	releases int
	markErr  error
}

func newScriptedDeduper() *scriptedDeduper {
	return &scriptedDeduper{seen: map[string]bool{}}
}

func (d *scriptedDeduper) MarkProcessed(_ context.Context, key string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.marks++
	if d.markErr != nil {
		return false, d.markErr
	}
	if d.seen[key] {
		return false, nil
	}
	d.seen[key] = true
	return true, nil
}

func (d *scriptedDeduper) Release(_ context.Context, key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.releases++
	delete(d.seen, key)
	return nil
}

func (d *scriptedDeduper) isClaimed(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.seen[key]
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

// newTestConsumer 装一个不会真的睡过去的消费者。
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

// 一次抖动不该弄丢一笔收货：重试之后成功，事件只被处理成功一次。
func TestATransientFailureIsRetriedInsteadOfLost(t *testing.T) {
	dedupe := newScriptedDeduper()
	var handled int
	c := newTestConsumer(nil, dedupe, func(context.Context, Envelope) error {
		handled++
		if handled < 3 {
			return errors.New("connection reset")
		}
		return nil
	})

	var env Envelope
	if err := json.Unmarshal(envelopeMsg(1).Value, &env); err != nil {
		t.Fatal(err)
	}
	if ok := c.handleOne(context.Background(), env); !ok {
		t.Fatal("重试之后成功了，却没让提交偏移量——这条事件会被无谓地再投一遍")
	}
	if handled != 3 {
		t.Fatalf("该重试到成功为止，实际只跑了 %d 次", handled)
	}
	if !dedupe.isClaimed(env.DedupeKey()) {
		t.Fatal("成功之后认领不该被还回去，否则重投会把这笔收货再记一遍")
	}
}

// 处理不了的事件要进死信表，然后让队列继续走。
//
// 两个都要：只停不走会把整个分区堵死，只走不停就是今天这个悄悄丢掉的 bug。
func TestAnUnprocessableEventIsParkedAndTheQueueMovesOn(t *testing.T) {
	dedupe := newScriptedDeduper()
	dead := &fakeDeadLetter{}
	c := newTestConsumer(nil, dedupe, func(context.Context, Envelope) error {
		return errors.New("IV_NO_WAREHOUSE: 没有可用仓库，无法收货")
	})
	c.dead = dead

	var env Envelope
	_ = json.Unmarshal(envelopeMsg(7).Value, &env)
	if ok := c.handleOne(context.Background(), env); !ok {
		t.Fatal("已经停进死信表了，就该提交偏移量让队列继续走")
	}
	parked := dead.all()
	if len(parked) != 1 {
		t.Fatalf("该停进死信表一条，实际 %d 条", len(parked))
	}
	if parked[0].tenant != 2 {
		t.Fatalf("死信要记得是哪家公司的，实际 %d", parked[0].tenant)
	}
	if parked[0].reason == "" {
		t.Fatal("死信没记原因，等于只知道丢了、不知道为什么")
	}
}

// 停都停不进去的时候，绝不能提交：既没干成、又没记下来的事件，正是这一整段
// 代码存在的理由。
func TestAnEventIsNotCommittedWhenParkingFails(t *testing.T) {
	dedupe := newScriptedDeduper()
	dead := &fakeDeadLetter{err: errors.New("database is down")}
	c := newTestConsumer(nil, dedupe, func(context.Context, Envelope) error {
		return errors.New("boom")
	})
	c.dead = dead

	var env Envelope
	_ = json.Unmarshal(envelopeMsg(9).Value, &env)
	if ok := c.handleOne(context.Background(), env); ok {
		t.Fatal("既没处理成功、也没记进死信表，却提交了偏移量——事件凭空消失")
	}
	if dedupe.isClaimed(env.DedupeKey()) {
		t.Fatal("放弃之后认领没还回去，重投时会被当成已处理直接跳过")
	}
}

// 没配死信表的消费者宁可堵住，也不能悄悄丢。
func TestWithoutADeadLetterTheEventIsKeptRatherThanDropped(t *testing.T) {
	dedupe := newScriptedDeduper()
	c := newTestConsumer(nil, dedupe, func(context.Context, Envelope) error {
		return errors.New("boom")
	})

	var env Envelope
	_ = json.Unmarshal(envelopeMsg(11).Value, &env)
	if ok := c.handleOne(context.Background(), env); ok {
		t.Fatal("没地方停放，却把事件提交掉了——这就是悄悄丢失")
	}
	if dedupe.isClaimed(env.DedupeKey()) {
		t.Fatal("认领没还回去，下次重投会被跳过，等于还是丢了")
	}
}

// 真的已经处理过的，跳过并提交——这是去重原本就该有的作用。
func TestAlreadyHandledEventIsSkippedNotRerun(t *testing.T) {
	dedupe := newScriptedDeduper()
	var handled int
	c := newTestConsumer(nil, dedupe, func(context.Context, Envelope) error {
		handled++
		return nil
	})

	var env Envelope
	_ = json.Unmarshal(envelopeMsg(13).Value, &env)
	if ok := c.handleOne(context.Background(), env); !ok {
		t.Fatal("第一次就该处理成功")
	}
	if ok := c.handleOne(context.Background(), env); !ok {
		t.Fatal("第二次该直接跳过并提交")
	}
	if handled != 1 {
		t.Fatalf("同一条事件被处理了 %d 次——库存会翻倍", handled)
	}
}

// 去重表本身查不了的时候，不能热转着重试。
func TestADedupeFailureDoesNotSpinHot(t *testing.T) {
	dedupe := newScriptedDeduper()
	dedupe.markErr = errors.New("database is down")
	var slept int
	c := newTestConsumer(nil, dedupe, func(context.Context, Envelope) error { return nil })
	c.sleep = func(context.Context, time.Duration) error { slept++; return nil }

	var env Envelope
	_ = json.Unmarshal(envelopeMsg(15).Value, &env)
	if ok := c.handleOne(context.Background(), env); ok {
		t.Fatal("查都没查到，不该提交偏移量")
	}
	// handleOne 自己不睡；睡在 Run 的循环里，这里断言的是它把决定权交了出去。
	if slept != 0 {
		t.Fatal("等待该发生在 Run 的循环里，而不是埋在 handleOne 中")
	}
}

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
