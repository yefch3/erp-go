// Package kafkain holds procurement's event consumers. This service learns
// what to buy only through here, and everything it needs is in the payload.
package kafkain

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/sgao19/erp-go/pkg/kafkax"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

// eventStockAllocated is the only event this consumer acts on.
//
// Note what it is NOT: ContractEffective. Procurement does not source from
// contracts, it sources from what stock could not cover. Listening to the
// contract directly would raise a requirement for goods already in the
// warehouse, which is the whole reason this indirection exists.
const eventStockAllocated = "StockAllocated"

// StockEvents raises purchase requirements for shortages.
func StockEvents(svc *app.Service, log *slog.Logger) kafkax.Handler {
	return func(ctx context.Context, e kafkax.Envelope) error {
		if e.EventType != eventStockAllocated {
			return nil
		}
		var a app.StockAllocated
		if err := json.Unmarshal(e.Payload, &a); err != nil {
			// Retrying cannot fix a payload we cannot read; parking the
			// consumer group on it would stop every later contract too.
			log.Error("stock event: unreadable payload, skipping",
				"event_id", e.EventID, "err", err)
			return nil
		}
		if a.ContractID == 0 {
			log.Error("stock event without a contract id, skipping", "event_id", e.EventID)
			return nil
		}
		return svc.RequirementsFromShortage(ctx, e.TenantID, a, log)
	}
}
