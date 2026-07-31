package kafkain

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/sgao19/erp-go/pkg/kafkax"
	"github.com/sgao19/erp-go/services/export/internal/app"
)

const eventStockOutbound = "StockOutbound"

// StockEvents records what the warehouse shipped against a contract.
//
// Before this the outbound event had no reader at all: the warehouse could
// ship a contract in full and the salesperson who sold it saw no trace of it
// anywhere — they had to telephone the warehouse to find out whether their
// customer's goods had left. Inventory knew, export did not, and the two
// halves of the same order never met.
func StockEvents(svc *app.Service, log *slog.Logger) kafkax.Handler {
	return func(ctx context.Context, e kafkax.Envelope) error {
		if e.EventType != eventStockOutbound {
			return nil // the same topic also carries allocation results
		}
		var o app.StockOutbound
		if err := json.Unmarshal(e.Payload, &o); err != nil {
			log.Error("outbound event: unreadable payload, skipping",
				"event_id", e.EventID, "err", err)
			return nil
		}
		return svc.ApplyShipment(ctx, e.TenantID, o, log)
	}
}
