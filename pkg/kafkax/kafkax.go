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

	"github.com/segmentio/kafka-go"
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
// MarkProcessed returns false when the event was already handled.
type Deduper interface {
	MarkProcessed(ctx context.Context, dedupeKey string) (fresh bool, err error)
}

// Consumer reads one topic within a consumer group, applying dedupe before
// the handler. Offsets commit only after successful handling, giving
// at-least-once delivery with exactly-once effect (dedupe + idempotent handlers).
type Consumer struct {
	r       *kafka.Reader
	dedupe  Deduper
	handler Handler
	log     *slog.Logger
}

func NewConsumer(brokers []string, group, topic string, d Deduper, h Handler, log *slog.Logger) *Consumer {
	return &Consumer{
		r: kafka.NewReader(kafka.ReaderConfig{
			Brokers:  brokers,
			GroupID:  group,
			Topic:    topic,
			MinBytes: 1,
			MaxBytes: 10 << 20,
			MaxWait:  500 * time.Millisecond,
		}),
		dedupe: d, handler: h, log: log,
	}
}

func (c *Consumer) Run(ctx context.Context) error {
	defer c.r.Close()
	for {
		msg, err := c.r.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return ctx.Err()
			}
			return fmt.Errorf("kafkax: fetch: %w", err)
		}
		var env Envelope
		if err := json.Unmarshal(msg.Value, &env); err != nil {
			// Malformed message: log and skip. Retrying cannot fix it.
			c.log.Error("kafkax: malformed event, skipping",
				"topic", msg.Topic, "offset", msg.Offset, "err", err)
			_ = c.r.CommitMessages(ctx, msg)
			continue
		}
		fresh, err := c.dedupe.MarkProcessed(ctx, env.DedupeKey())
		if err != nil {
			c.log.Error("kafkax: dedupe check failed, will retry", "err", err)
			continue // no commit -> redelivered
		}
		if fresh {
			if err := c.handler(ctx, env); err != nil {
				c.log.Error("kafkax: handler failed, will retry",
					"event", env.EventType, "aggregate", env.AggregateID, "err", err)
				continue // no commit -> redelivered
			}
		}
		if err := c.r.CommitMessages(ctx, msg); err != nil {
			c.log.Warn("kafkax: offset commit failed", "err", err)
		}
	}
}
