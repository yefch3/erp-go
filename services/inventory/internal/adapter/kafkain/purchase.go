package kafkain

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/sgao19/erp-go/pkg/kafkax"
	"github.com/sgao19/erp-go/services/inventory/internal/app"
)

const eventPurchaseReceived = "PurchaseReceived"

// PurchaseEvents puts delivered goods into stock.
//
// The receipt was recorded in procurement's database and the stock increase
// happens in this one, so they cannot share a transaction. The event is what
// bridges them: procurement wrote it in the same transaction as the receipt,
// so either both are durable or neither is, and a redelivery is absorbed by
// the consumer's dedupe table.
//
// The interesting part is what happens afterwards without anybody arranging
// it. Receiving stock already hands new goods to the contracts waiting for
// them and re-announces the shrunken shortage, so a delivery closes the
// purchase requirements it covers — including ones raised for other contracts
// entirely — with procurement knowing nothing about reservations.
func PurchaseEvents(svc *app.Service, log *slog.Logger) kafkax.Handler {
	return func(ctx context.Context, e kafkax.Envelope, claim kafkax.Claim) error {
		if e.EventType != eventPurchaseReceived {
			return nil
		}
		var p app.PurchaseReceived
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			log.Error("purchase event: unreadable payload, skipping",
				"event_id", e.EventID, "err", err)
			return nil
		}
		if len(p.Lines) == 0 {
			log.Warn("purchase receipt with no lines, nothing to receive",
				"event_id", e.EventID, "receipt_no", p.ReceiptNo)
			return nil
		}
		return svc.ReceivePurchase(ctx, e.TenantID, p, log, app.EventClaim(claim))
	}
}
