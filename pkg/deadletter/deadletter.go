// Package deadletter parks events a consumer could not handle, so that
// giving up is recorded rather than silent.
//
// A queue has three possible answers to "this event will not process": lose
// it, block on it for ever, or set it aside where somebody can see it. The
// first two are the ones that happen by accident — losing it looks like
// nothing happened, blocking looks like nothing happened. Only the third
// leaves evidence, which is the whole point of the table.
package deadletter

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store writes into the failed_events table the consuming service owns.
type Store struct {
	pool  *pgxpool.Pool
	group string
}

func New(pool *pgxpool.Pool, consumerGroup string) *Store {
	return &Store{pool: pool, group: consumerGroup}
}

// Park records one event that will not be retried again.
//
// Keyed the same way dedupe is, so parking twice is not an error: a
// redelivery after an offset commit failed must not fail here as well.
func (s *Store) Park(ctx context.Context, tenantID int64, dedupeKey, topic, eventType, aggregateID string, payload []byte, reason string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO failed_events (
			tenant_id, event_id, consumer_group, topic, event_type,
			aggregate_id, payload, reason
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (event_id, consumer_group) DO NOTHING`,
		tenantID, dedupeKey, s.group, topic, eventType, aggregateID, payload, reason)
	if err != nil {
		return fmt.Errorf("deadletter: park: %w", err)
	}
	return nil
}

// DDL is embedded verbatim by every consuming service's migrations, the same
// way pkg/idempotency does it.
//
// tenant_id is carried even though nothing filters by it yet: a parked event
// belongs to one company, and "which company lost a receipt" is the first
// question anybody will ask of this table.
const DDL = `
CREATE TABLE IF NOT EXISTS failed_events (
    id             BIGSERIAL    PRIMARY KEY,
    tenant_id      BIGINT       NOT NULL DEFAULT 1,
    event_id       VARCHAR(200) NOT NULL,
    consumer_group VARCHAR(100) NOT NULL,
    topic          VARCHAR(200) NOT NULL DEFAULT '',
    event_type     VARCHAR(200) NOT NULL DEFAULT '',
    aggregate_id   VARCHAR(200) NOT NULL DEFAULT '',
    payload        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    reason         TEXT         NOT NULL DEFAULT '',
    parked_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (event_id, consumer_group)
);
`

// Parked is one event as it sits in the table.
type Parked struct {
	ID            int64
	TenantID      int64
	EventKey      string
	ConsumerGroup string
	Topic         string
	EventType     string
	AggregateID   string
	Payload       []byte
	Reason        string
	ParkedAt      time.Time
}

// List returns everything this consumer group has given up on, newest first.
//
// No tenant filter on purpose: this is an operations view, read by whoever
// runs the platform, and "which companies are losing events" is exactly what
// it exists to answer. The gateway gates it behind the platform-operator list.
func (s *Store) List(ctx context.Context, limit int32) ([]Parked, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, event_id, consumer_group, topic, event_type,
		       aggregate_id, payload, reason, parked_at
		FROM failed_events
		WHERE consumer_group = $1
		ORDER BY parked_at DESC LIMIT $2`, s.group, limit)
	if err != nil {
		return nil, fmt.Errorf("deadletter: list: %w", err)
	}
	defer rows.Close()
	var out []Parked
	for rows.Next() {
		var p Parked
		if err := rows.Scan(&p.ID, &p.TenantID, &p.EventKey, &p.ConsumerGroup,
			&p.Topic, &p.EventType, &p.AggregateID, &p.Payload, &p.Reason, &p.ParkedAt); err != nil {
			return nil, fmt.Errorf("deadletter: scan: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Replay runs one parked event through the handler again and, on success,
// takes it off the table.
//
// Synchronous and in-process rather than re-published to the queue, because
// the publish route has an unfixable ordering problem: the claim has to be
// released before the message can be re-consumed, and between those two steps
// a crash leaves the event neither claimed nor parked — invisible again,
// which is the one outcome this whole mechanism exists to prevent. Running
// the handler here keeps the row in place until the work has actually
// happened, and hands the error straight back to the person clicking the
// button — the only moment somebody is guaranteed to be looking.
//
// The claim in processed_events is left alone: it was taken when the event
// first arrived and the event is now genuinely processed, which is what a
// claim means. A crash after the handler commits but before the row is
// deleted leaves a stale parked row for an already-done event; replaying THAT
// would run the handler twice, so the operator seeing "重放失败" twice for
// the same row should check the business record before a third try. Narrow,
// stated, and strictly better than the queue's own crash window.
func (s *Store) Replay(ctx context.Context, id int64, handle func(ctx context.Context, e Envelope) error) error {
	var p Parked
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, event_id, topic, event_type, aggregate_id, payload
		FROM failed_events WHERE id = $1 AND consumer_group = $2`, id, s.group).
		Scan(&p.ID, &p.TenantID, &p.EventKey, &p.Topic, &p.EventType, &p.AggregateID, &p.Payload)
	if err != nil {
		return fmt.Errorf("deadletter: load %d: %w", id, err)
	}
	env, err := envelopeOf(p)
	if err != nil {
		return err
	}
	if err := handle(ctx, env); err != nil {
		return fmt.Errorf("重放仍然失败：%w", err)
	}
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM failed_events WHERE id = $1 AND consumer_group = $2`, id, s.group); err != nil {
		return fmt.Errorf("deadletter: clear %d: %w", id, err)
	}
	return nil
}

// Envelope mirrors kafkax.Envelope without importing it: kafkax already
// depends on this package's interface, and Go allows no cycles.
type Envelope struct {
	EventID       int64
	TenantID      int64
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       []byte
}

// envelopeOf rebuilds the envelope the handler originally saw. The dedupe key
// is "aggregate_type:event_id" (kafkax.DedupeKey), carried in event_id.
func envelopeOf(p Parked) (Envelope, error) {
	i := strings.LastIndex(p.EventKey, ":")
	if i < 1 {
		return Envelope{}, fmt.Errorf("deadletter: %d carries an unreadable event key %q", p.ID, p.EventKey)
	}
	n, err := strconv.ParseInt(p.EventKey[i+1:], 10, 64)
	if err != nil {
		return Envelope{}, fmt.Errorf("deadletter: %d carries an unreadable event id in %q", p.ID, p.EventKey)
	}
	return Envelope{
		EventID: n, TenantID: p.TenantID, AggregateType: p.EventKey[:i],
		AggregateID: p.AggregateID, EventType: p.EventType, Payload: p.Payload,
	}, nil
}

// Console 把一个服务的若干消费组死信聚成一个运维面：列出所有被放弃的事件、
// 按组把一条重放一遍。每个消费服务的 gRPC 层挂一个，网关再把三个服务的
// 汇成一页。
type Console struct {
	sources []consoleSource
}

type consoleSource struct {
	group  string
	store  *Store
	handle func(ctx context.Context, e Envelope) error
}

func NewConsole() *Console { return &Console{} }

func (c *Console) Add(group string, store *Store, handle func(ctx context.Context, e Envelope) error) {
	c.sources = append(c.sources, consoleSource{group: group, store: store, handle: handle})
}

// List unions every group's parked events, newest first.
func (c *Console) List(ctx context.Context) ([]Parked, error) {
	var out []Parked
	for _, src := range c.sources {
		rows, err := src.store.List(ctx, 200)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ParkedAt.After(out[j].ParkedAt) })
	return out, nil
}

// Replay dispatches to the group that parked the event.
func (c *Console) Replay(ctx context.Context, group string, id int64) error {
	for _, src := range c.sources {
		if src.group == group {
			return src.store.Replay(ctx, id, src.handle)
		}
	}
	return fmt.Errorf("deadletter: unknown consumer group %q", group)
}
