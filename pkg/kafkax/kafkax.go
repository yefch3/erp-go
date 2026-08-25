// Package kafkax wraps segmentio/kafka-go with the two shapes this system
// uses: a producer for the outbox relay, and a consumer that enforces
// per-event idempotency before invoking the handler.
package kafkax

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/sgao19/erp-go/pkg/deadletter"
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

// Handler processes one event. Returning an error means "retry later":
// the consumer does not commit the offset.
type Handler func(ctx context.Context, e Envelope) error

// Deduper is implemented with the processed_events table (pkg/idempotency).
// MarkProcessed returns false when the event was already handled; Release
// takes the mark back off when the handler did not finish, so the attempt
// can be made again.
type Deduper interface {
	MarkProcessed(ctx context.Context, dedupeKey string) (fresh bool, err error)
	Release(ctx context.Context, dedupeKey string) error
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
// The order of the two writes here is the whole point. The mark used to be
// taken BEFORE the handler and never given back, so a handler that returned
// an error left the event marked done; the redelivery saw the mark, skipped
// the handler and committed. One failed attempt and the event was gone — a
// receipt of goods, a contract taking effect — with a log line claiming it
// would be retried. Nothing on any screen could show it.
//
// The mark is still taken first, and that is deliberate rather than an
// oversight: it is what stops the same event being applied twice. None of
// these handlers is idempotent — booking a delivery into stock twice doubles
// the stock — so the claim is held for the whole attempt and only released
// when the work definitely did not happen. A handler's error means its
// transaction rolled back, so there is nothing half-applied for the retry to
// duplicate.
//
// What this deliberately does NOT fix: a process killed hard (OOM, kill -9,
// node death) between taking the claim and the handler's COMMIT loses the
// event — the claim stays, the work never happened, and the redelivery sees
// "done" and moves on. Note the asymmetry: a crash AFTER the handler commits
// is safe (redelivery skips work that genuinely happened), and an error
// return is safe (the claim is released below). Only the hard-kill during
// the attempt loses. Closing that window for real means writing the claim
// inside each handler's own transaction — a change to every handler, not to
// this loop — and is written up in the plan as its own piece of work.
func (c *Consumer) handleOne(ctx context.Context, env Envelope) bool {
	key := env.DedupeKey()
	fresh, err := c.dedupe.MarkProcessed(ctx, key)
	if err != nil {
		c.log.Error("kafkax: dedupe check failed, will retry", "err", err)
		return false
	}
	if !fresh {
		return true // genuinely handled before; committing is how we move on
	}

	var lastErr error
	for attempt := 1; attempt <= handlerAttempts; attempt++ {
		if lastErr = c.handler(ctx, env); lastErr == nil {
			return true
		}
		c.log.Warn("kafkax: handler failed, retrying",
			"event", env.EventType, "aggregate", env.AggregateID,
			"attempt", attempt, "of", handlerAttempts, "err", lastErr)
		if attempt == handlerAttempts {
			break
		}
		if err := c.sleep(ctx, backoffFor(attempt)); err != nil {
			// Shutting down mid-retry: hand the event back so the next
			// process picks it up rather than inheriting our claim.
			c.release(ctx, key)
			return false
		}
	}

	if c.dead == nil {
		// Nowhere to put it. Keep retrying rather than dropping it — this
		// blocks the partition, which is bad, but a stall is recoverable and
		// a silent loss is not.
		c.log.Error("kafkax: handler keeps failing and no dead-letter store is configured, will keep retrying",
			"event", env.EventType, "aggregate", env.AggregateID, "err", lastErr)
		c.release(ctx, key)
		return false
	}
	if err := c.dead.Park(ctx, env.TenantID, key, c.topic, env.EventType,
		env.AggregateID, env.Payload, lastErr.Error()); err != nil {
		// Could not even record giving up. Do not commit: an event that is
		// neither done nor written down anywhere is exactly what this whole
		// function exists to prevent.
		c.log.Error("kafkax: could not park a failed event, will retry", "err", err)
		c.release(ctx, key)
		return false
	}
	c.log.Error("kafkax: event set aside after repeated failures — see failed_events",
		"event", env.EventType, "aggregate", env.AggregateID,
		"tenant", env.TenantID, "err", lastErr)
	return true
}

// release hands the claim back, and says so loudly if it cannot: a claim left
// behind is an event that will never run again.
func (c *Consumer) release(ctx context.Context, key string) {
	if err := c.dedupe.Release(context.WithoutCancel(ctx), key); err != nil {
		c.log.Error("kafkax: could not release a failed event's claim, it will be skipped on redelivery",
			"key", key, "err", err)
	}
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
func ReplayHandler(h Handler) func(context.Context, deadletter.Envelope) error {
	return func(ctx context.Context, e deadletter.Envelope) error {
		return h(ctx, Envelope{
			EventID: e.EventID, TenantID: e.TenantID,
			AggregateType: e.AggregateType, AggregateID: e.AggregateID,
			EventType: e.EventType, Payload: e.Payload,
		})
	}
}
