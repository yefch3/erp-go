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
