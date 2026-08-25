// Package kafkax wraps segmentio/kafka-go with the two shapes this system
// uses: a producer for the outbox relay, and a consumer that enforces
// per-event idempotency before invoking the handler.
package kafkax

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/segmentio/kafka-go"

	"github.com/sgao19/erp-go/pkg/deadletter"
	"github.com/sgao19/erp-go/pkg/idempotency"
)

// Producer publishes messages with the aggregate id as the partition key.
type Producer struct {
	w *kafka.Writer
}

func NewProducer(brokers []string) *Producer {
	return &Producer{w: &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Balancer:     &kafka.Hash{}, // key-based partitioning keeps per-aggregate order
		RequiredAcks: kafka.RequireAll,
		BatchTimeout: 20 * time.Millisecond,
		// Topics are pre-created by ops/compose; auto-create hides typos.
		AllowAutoTopicCreation: false,
	}}
}

func (p *Producer) Publish(ctx context.Context, topic string, key, value []byte) error {
	return p.w.WriteMessages(ctx, kafka.Message{Topic: topic, Key: key, Value: value})
}

func (p *Producer) Close() error { return p.w.Close() }

// Envelope mirrors outbox.wireEvent on the consuming side.
type Envelope struct {
	EventID       int64           `json:"event_id"`
	TenantID      int64           `json:"tenant_id"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	EventType     string          `json:"event_type"`
	Payload       json.RawMessage `json:"payload"`
	TraceID       string          `json:"trace_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

// DedupeKey identifies an event globally: outbox ids are only unique per
// producing service, so the aggregate type disambiguates.
func (e Envelope) DedupeKey() string {
	return fmt.Sprintf("%s:%d", e.AggregateType, e.EventID)
}

// Claim 记下「这条事件我处理了」，**必须由 handler 在它自己干活的那个事务
// 里调用**。这是整套机制的要点：认领和业务写入同生共死。
//
// 不调用也不算错——handler 因为事件类型不匹配而直接跳过时，本来就没有事务，
// 也没有任何东西需要被记住。
type Claim func(ctx context.Context, tx pgx.Tx) error

// Handler processes one event. Returning an error means "retry later":
// the consumer does not commit the offset.
type Handler func(ctx context.Context, e Envelope, claim Claim) error

// Deduper is implemented with the processed_events table (pkg/idempotency).
//
// 两个方法分工明确：AlreadyProcessed 是只读快路径（干过就直接提交偏移量），
// ClaimInTx 是真正的把关——它写在 handler 自己的事务里，靠唯一约束在并发时
// 只让一个人成功。
type Deduper interface {
	AlreadyProcessed(ctx context.Context, dedupeKey string) (bool, error)
	ClaimInTx(ctx context.Context, tx pgx.Tx, dedupeKey string) error
}

// DeadLetter is where an event goes once retrying it has stopped being
// useful. Optional: a consumer without one keeps the event uncommitted and
// therefore keeps retrying it for ever, which is loud but blocking.
type DeadLetter interface {
	Park(ctx context.Context, tenantID int64, dedupeKey, topic, eventType, aggregateID string, payload []byte, reason string) error
}

// reader is the slice of *kafka.Reader this package uses. It exists so tests
// can drive the fetch-error path directly: the failure that matters (a broker
// that disappears mid-session) cannot be provoked from outside, because a
// broker unreachable from the start is retried inside kafka-go and never
// surfaces an error here at all.
type reader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// Consumer reads one topic within a consumer group, applying dedupe before
// the handler. Offsets commit only after successful handling, giving
// at-least-once delivery with exactly-once effect (dedupe + idempotent handlers).
type Consumer struct {
	r            reader
	topic, group string
	dedupe       Deduper
	dead         DeadLetter
	handler      Handler
	log          *slog.Logger
	// Swapped out in tests so a retry scenario does not take real seconds.
	sleep func(context.Context, time.Duration) error
}

// NewConsumer wires one topic.
//
// dead is a parameter rather than an optional setter on purpose. Somewhere to
// put an event that will never succeed is not a nicety — without it the only
// choices left are dropping it or blocking the partition for ever. A setter
// is a step somebody eventually forgets, and this whole file exists because
// of a step nobody noticed was missing. Passing nil is allowed and honest:
// it means "block rather than drop", and it says so at the call site.
func NewConsumer(brokers []string, group, topic string, d Deduper, dead DeadLetter, h Handler, log *slog.Logger) *Consumer {
	return &Consumer{
		r: kafka.NewReader(kafka.ReaderConfig{
			Brokers:  brokers,
			GroupID:  group,
			Topic:    topic,
			MinBytes: 1,
			MaxBytes: 10 << 20,
			MaxWait:  500 * time.Millisecond,
		}),
		topic: topic, group: group,
		dedupe: d, dead: dead, handler: h, log: log,
		sleep: sleepCtx,
	}
}

// Backoff bounds for reconnecting after a fetch failure. The ceiling is low
// on purpose: the cost of retrying a dead broker is one TCP handshake, and
// the cost of being slow to notice it came back is unprocessed events.
const (
	fetchRetryMin = 500 * time.Millisecond
	fetchRetryMax = 30 * time.Second
)

func (c *Consumer) Run(ctx context.Context) error {
	defer c.r.Close()
	var retry time.Duration
	for {
		msg, err := c.r.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// A broker restart, a leader election or a network blip all land
			// here, and none of them mean "stop consuming forever". Returning
			// used to do exactly that: the goroutine ended, the service stayed
			// up and healthy, and the only trace was one ERROR line at the
			// moment of the disconnect. The symptom surfaced days later as
			// "the contract took effect but stock never moved".
			retry = nextRetry(retry)
			c.log.Warn("kafkax: fetch failed, reconnecting",
				"topic", c.topic, "group", c.group, "retry_in", retry, "err", err)
			select {
			case <-time.After(retry):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		retry = 0
		var env Envelope
		if err := json.Unmarshal(msg.Value, &env); err != nil {
			// Malformed message: log and skip. Retrying cannot fix it.
			c.log.Error("kafkax: malformed event, skipping",
				"topic", msg.Topic, "offset", msg.Offset, "err", err)
			_ = c.r.CommitMessages(ctx, msg)
			continue
		}
		if !c.handleOne(ctx, env) {
			// Left uncommitted on purpose: the same message comes back.
			// Waiting first, because "fetch it again immediately" is a hot
			// loop against the database that just failed.
			if err := c.sleep(ctx, fetchRetryMin); err != nil {
				return err
			}
			continue
		}
		if err := c.r.CommitMessages(ctx, msg); err != nil {
			c.log.Warn("kafkax: offset commit failed", "err", err)
		}
	}
}

// Attempts before an event is set aside. Small on purpose: these retries are
// for the blip — a dropped connection, a lock timeout — and a fault that
// outlives half a minute is not going to be fixed by a seventh try.
const handlerAttempts = 5

// handleOne runs one event to a conclusion and reports whether the offset may
// be committed.
//
// 认领写在 handler 自己的事务里（transactional inbox），这是这段代码的
// 全部要点。历史上它经过两版：
//
// 一版：认领在 handler **之前**单独提交、失败也不撤销。于是 handler 只要
// 返回一次错误，事件就永远消失——重投时认领在，跳过，提交偏移量。一次数据库
// 抖动就能吃掉一笔收货，而日志写着 will retry。
//
// 二版：认领仍在前面，但失败时归还。常见故障（handler 返回错误）不再丢，
// 可硬杀（OOM / kill -9 / 宕机）还是丢——认领已提交、活还没提交，重投时
// 跳过一件从没干完的事。窗口是整个处理时长。
//
// 现在这一版把认领交给 handler，在它干活的同一个事务里写。三种时刻被杀的
// 结果都是安全的：
//
//	事务提交前被杀   → 认领随事务回滚，重投时从头再来（不丢）
//	事务提交后被杀   → 认领在，重投时跳过真正干完的活（不重）
//	偏移量提交失败   → 同上，重投时跳过（不重）
//
// 代价是 handler 的签名多一个参数，且必须在事务里调用它一次。作为交换，
// 「事件悄悄消失」这一整类故障没有了。
func (c *Consumer) handleOne(ctx context.Context, env Envelope) bool {
	key := env.DedupeKey()
	done, err := c.dedupe.AlreadyProcessed(ctx, key)
	if err != nil {
		c.log.Error("kafkax: dedupe check failed, will retry", "err", err)
		return false
	}
	if done {
		return true // 真的干过了；提交偏移量是往前走的方式
	}
	claim := func(ctx context.Context, tx pgx.Tx) error {
		return c.dedupe.ClaimInTx(ctx, tx, key)
	}

	var lastErr error
	for attempt := 1; attempt <= handlerAttempts; attempt++ {
		lastErr = c.handler(ctx, env, claim)
		if lastErr == nil {
			return true
		}
		// 并发的另一个消费者抢先干完了（重平衡的瞬间会有）。它已经连同认领
		// 一起提交，我们这边的事务已经回滚——这条事件是干成了的，往前走。
		if errors.Is(lastErr, idempotency.ErrAlreadyClaimed) {
			return true
		}
		c.log.Warn("kafkax: handler failed, retrying",
			"event", env.EventType, "aggregate", env.AggregateID,
			"attempt", attempt, "of", handlerAttempts, "err", lastErr)
		if attempt == handlerAttempts {
			break
		}
		if err := c.sleep(ctx, backoffFor(attempt)); err != nil {
			// 停机中途：什么都没提交过，直接放手，下一个进程从头再来。
			return false
		}
	}

	if c.dead == nil {
		// 没地方停放。宁可堵住也不丢：堵是可恢复的，丢不是。
		c.log.Error("kafkax: handler keeps failing and no dead-letter store is configured, will keep retrying",
			"event", env.EventType, "aggregate", env.AggregateID, "err", lastErr)
		return false
	}
	if err := c.dead.Park(ctx, env.TenantID, key, c.topic, env.EventType,
		env.AggregateID, env.Payload, lastErr.Error()); err != nil {
		// 既没干成、又没记下来的事件，正是这一整段代码存在的理由。
		c.log.Error("kafkax: could not park a failed event, will retry", "err", err)
		return false
	}
	c.log.Error("kafkax: event set aside after repeated failures — see failed_events",
		"event", env.EventType, "aggregate", env.AggregateID,
		"tenant", env.TenantID, "err", lastErr)
	return true
}

// backoffFor spaces out the retries: 1s, 2s, 4s, 8s.
func backoffFor(attempt int) time.Duration {
	d := time.Second
	for i := 1; i < attempt; i++ {
		if d *= 2; d > fetchRetryMax {
			return fetchRetryMax
		}
	}
	return d
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// nextRetry doubles the wait up to the ceiling, starting from the floor.
func nextRetry(d time.Duration) time.Duration {
	if d == 0 {
		return fetchRetryMin
	}
	if d *= 2; d > fetchRetryMax {
		return fetchRetryMax
	}
	return d
}

// ReplayHandler adapts a consumer's handler for the dead-letter console, so
// a replayed event runs through exactly the code that failed it — same
// parsing, same guards, same transaction.
//
// 重放同样在事务里认领：停进死信表的事件从来没被认领过（认领只随成功的业务
// 事务一起提交），所以重放就是正常地跑一遍，成功即认领。
func ReplayHandler(h Handler, d Deduper) func(context.Context, deadletter.Envelope) error {
	return func(ctx context.Context, e deadletter.Envelope) error {
		env := Envelope{
			EventID: e.EventID, TenantID: e.TenantID,
			AggregateType: e.AggregateType, AggregateID: e.AggregateID,
			EventType: e.EventType, Payload: e.Payload,
		}
		key := env.DedupeKey()
		return h(ctx, env, func(ctx context.Context, tx pgx.Tx) error {
			return d.ClaimInTx(ctx, tx, key)
		})
	}
}
