// Package outbox implements the transactional outbox: domain changes and
// their events commit in one local transaction, and a relay goroutine
// delivers the events to Kafka afterwards. Services never call the Kafka
// producer inside a business transaction.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Event is one row in outbox_events. Payload must be self-contained JSON:
// consumers do not call back into the producer to complete the picture.
type Event struct {
	ID            int64
	TenantID      int64
	AggregateType string // "contract", "shipment_plan", ...
	AggregateID   string // used as the Kafka partition key
	EventType     string // "ContractEffective", ...
	Payload       json.RawMessage
	TraceID       string
	CreatedAt     time.Time
}

// Append writes the event inside the caller's transaction. This is the only
// legal way to publish: it makes the DB change and the event atomic.
//
// TenantID is required, and a missing one is an error rather than a default.
//
// It used to default to 1. That is the shape of a bug this codebase has
// already paid for once: iam migration 00055 left tenant_id out of an INSERT
// column list, the column's own DEFAULT 1 filled it in, and three companies'
// sales roles silently lost two permissions for three weeks (fixed by 00081).
// Here the same trap is one line of Go away — a caller who omits the field
// gets int64's zero value, and nothing complains. The consumer on the other
// side acts on the envelope's tenant, not on anything inside the payload, so
// a letter stamped with the wrong company writes to the wrong company's
// inventory, orders and contracts without a single error anywhere.
//
// Refusing costs the person who forgot one failing test; defaulting costs
// somebody else a week of forensics months later.
func Append(ctx context.Context, tx pgx.Tx, e Event) error {
	if e.AggregateType == "" || e.AggregateID == "" || e.EventType == "" {
		return fmt.Errorf("outbox: aggregate_type, aggregate_id and event_type are required")
	}
	if e.TenantID <= 0 {
		return fmt.Errorf("outbox: tenant_id is required (%s/%s %s)",
			e.AggregateType, e.AggregateID, e.EventType)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (tenant_id, aggregate_type, aggregate_id, event_type, payload, trace_id)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		e.TenantID, e.AggregateType, e.AggregateID, e.EventType, e.Payload, e.TraceID)
	if err != nil {
		return fmt.Errorf("outbox: append: %w", err)
	}
	return nil
}

// DDL is the canonical outbox table, identical in every service database.
// Migrations embed it verbatim so the relay can rely on the shape.
const DDL = `
CREATE TABLE IF NOT EXISTS outbox_events (
    id             BIGSERIAL PRIMARY KEY,
    tenant_id      BIGINT      NOT NULL,
    aggregate_type TEXT        NOT NULL,
    aggregate_id   TEXT        NOT NULL,
    event_type     TEXT        NOT NULL,
    payload        JSONB       NOT NULL,
    trace_id       TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at   TIMESTAMPTZ,
    attempts       INT         NOT NULL DEFAULT 0,
    last_error     TEXT
);
CREATE INDEX IF NOT EXISTS outbox_events_unpublished_idx
    ON outbox_events (created_at) WHERE published_at IS NULL;
`
