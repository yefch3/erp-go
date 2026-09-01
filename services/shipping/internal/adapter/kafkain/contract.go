package kafkain

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/sgao19/erp-go/pkg/kafkax"
	"github.com/sgao19/erp-go/services/shipping/internal/app"
)

func ContractEvents(svc *app.Service, log *slog.Logger) kafkax.Handler {
	return func(ctx context.Context, envelope kafkax.Envelope, claim kafkax.Claim) error {
		if envelope.EventType != "ContractEffective" {
			return nil
		}
		var event app.ContractEffective
		if err := json.Unmarshal(envelope.Payload, &event); err != nil {
			log.Error("contract event: unreadable payload, skipping", "event_id", envelope.EventID, "err", err)
			return nil
		}
		if event.ContractID == 0 {
			log.Error("contract event without contract id, skipping", "event_id", envelope.EventID)
			return nil
		}
		return svc.HandoffsFromContract(ctx, envelope.TenantID, event, app.EventClaim(claim))
	}
}
