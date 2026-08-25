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

// Release takes the mark back off an event whose handler did not finish.
//
// Without this, "已处理" was recorded before the work was attempted, so a
// handler that returned an error left the event marked done. The redelivery
// then found the mark, skipped the handler and committed the offset — one
// failed attempt and the event was gone for good, while the log line said
// "will retry". A receipt of goods disappearing that way is invisible from
// every screen in the system.
//
// Safe to call because every handler does its work in one transaction: an
// error means that transaction rolled back, so there is nothing half-done
// for a retry to duplicate.
func (s *Store) Release(ctx context.Context, dedupeKey string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM processed_events
		WHERE event_id = $1 AND consumer_group = $2`, dedupeKey, s.group)
	if err != nil {
		return fmt.Errorf("idempotency: release: %w", err)
	}
	return nil
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
