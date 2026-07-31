// Package kafkain holds inventory's event consumers.
package kafkain

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/sgao19/erp-go/pkg/kafkax"
	"github.com/sgao19/erp-go/services/inventory/internal/app"
)

const eventContractEffective = "ContractEffective"

// ContractEvents claims stock for contracts that took effect. Inventory is
// the first thing to hear about an effective contract, not procurement: what
// has to be bought is whatever stock could not cover, and only this service
// can answer that without racing.
func ContractEvents(svc *app.Service, log *slog.Logger) kafkax.Handler {
	return func(ctx context.Context, e kafkax.Envelope) error {
		if e.EventType != eventContractEffective {
			return nil
		}
		var c app.ContractEffective
		if err := json.Unmarshal(e.Payload, &c); err != nil {
			log.Error("contract event: unreadable payload, skipping",
				"event_id", e.EventID, "err", err)
			return nil
		}
		if c.ContractID == 0 {
			log.Error("contract event without a contract id, skipping", "event_id", e.EventID)
			return nil
		}
		return svc.AllocateContract(ctx, e.TenantID, c, log)
	}
}
