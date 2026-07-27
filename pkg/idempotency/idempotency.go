// Package idempotency provides consumer-side event dedupe backed by the
// processed_events table each consuming service owns.
package idempotency

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store satisfies kafkax.Deduper for one consumer group.
type Store struct {
	pool  *pgxpool.Pool
	group string
}

func New(pool *pgxpool.Pool, consumerGroup string) *Store {
	return &Store{pool: pool, group: consumerGroup}
}

// MarkProcessed records the event and reports whether it was seen for the
// first time. ON CONFLICT DO NOTHING makes the check-and-set atomic.
func (s *Store) MarkProcessed(ctx context.Context, dedupeKey string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO processed_events (event_id, consumer_group)
		VALUES ($1, $2)
		ON CONFLICT (event_id, consumer_group) DO NOTHING`, dedupeKey, s.group)
	if err != nil {
		return false, fmt.Errorf("idempotency: mark: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// DDL is embedded verbatim by every consuming service's migrations.
const DDL = `
CREATE TABLE IF NOT EXISTS processed_events (
    event_id       VARCHAR(200) NOT NULL,
    consumer_group VARCHAR(100) NOT NULL,
    processed_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, consumer_group)
);
`
