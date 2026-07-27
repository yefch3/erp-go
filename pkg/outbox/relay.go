package outbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Publisher is what the relay needs from Kafka; satisfied by kafkax.Producer.
type Publisher interface {
	Publish(ctx context.Context, topic string, key, value []byte) error
}

// TopicResolver maps an aggregate type to its topic, e.g.
// "contract" -> "erp.export.contract.v1".
type TopicResolver func(aggregateType string) string

type Relay struct {
	pool      *pgxpool.Pool
	producer  Publisher
	topicFor  TopicResolver
	log       *slog.Logger
	interval  time.Duration
	batchSize int
}

func NewRelay(pool *pgxpool.Pool, producer Publisher, topicFor TopicResolver, log *slog.Logger) *Relay {
	return &Relay{
		pool: pool, producer: producer, topicFor: topicFor, log: log,
		interval: 500 * time.Millisecond, batchSize: 100,
	}
}

// Run polls until the context is cancelled. Start it as a goroutine next to
// the gRPC server; single instance per service is enough (FOR UPDATE
// SKIP LOCKED keeps multiple replicas safe anyway).
func (r *Relay) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.publishBatch(ctx); err != nil && ctx.Err() == nil {
				r.log.Error("outbox: batch failed", "err", err)
			}
		}
	}
}

// wireEvent is the envelope every consumer receives.
type wireEvent struct {
	EventID       int64           `json:"event_id"`
	TenantID      int64           `json:"tenant_id"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	EventType     string          `json:"event_type"`
	Payload       json.RawMessage `json:"payload"`
	TraceID       string          `json:"trace_id,omitempty"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

func (r *Relay) publishBatch(ctx context.Context) error {
	// Claim a batch with SKIP LOCKED so concurrent relays never double-send
	// within the same claim window.
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, aggregate_type, aggregate_id, event_type, payload, coalesce(trace_id,''), created_at
		FROM outbox_events
		WHERE published_at IS NULL
		ORDER BY created_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, r.batchSize)
	if err != nil {
		return err
	}
	events := make([]Event, 0, r.batchSize)
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TenantID, &e.AggregateType, &e.AggregateID,
			&e.EventType, &e.Payload, &e.TraceID, &e.CreatedAt); err != nil {
			rows.Close()
			return err
		}
		events = append(events, e)
	}
	rows.Close()
	if rows.Err() != nil {
		return rows.Err()
	}

	for _, e := range events {
		body, err := json.Marshal(wireEvent{
			EventID: e.ID, TenantID: e.TenantID,
			AggregateType: e.AggregateType, AggregateID: e.AggregateID,
			EventType: e.EventType, Payload: e.Payload,
			TraceID: e.TraceID, OccurredAt: e.CreatedAt,
		})
		if err != nil {
			_ = r.markFailed(ctx, e.ID, err)
			continue
		}
		// Partition key = aggregate id: events for one business object stay ordered.
		if err := r.producer.Publish(ctx, r.topicFor(e.AggregateType), []byte(e.AggregateID), body); err != nil {
			_ = r.markFailed(ctx, e.ID, err)
			continue
		}
		if _, err := r.pool.Exec(ctx,
			`UPDATE outbox_events SET published_at = now() WHERE id = $1`, e.ID); err != nil {
			// Kafka got the event but the mark failed: the next pass re-sends.
			// That is the at-least-once contract; consumers dedupe by event_id.
			r.log.Warn("outbox: publish succeeded but mark failed; will re-send", "id", e.ID, "err", err)
		}
	}
	return nil
}

func (r *Relay) markFailed(ctx context.Context, id int64, cause error) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE outbox_events SET attempts = attempts + 1, last_error = $2 WHERE id = $1`,
		id, cause.Error())
	return err
}
