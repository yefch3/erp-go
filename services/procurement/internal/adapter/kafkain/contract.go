// Package kafkain holds procurement's event consumers.
package kafkain

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/kafkax"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

const (
	eventContractEffective = "ContractEffective"
	eventQuotationAccepted = "QuotationAccepted"
	eventQuotationRejected = "QuotationRejected"
)

// ContractEvents raises the full purchase quantity from an effective customer
// contract. Inventory allocation is intentionally not consulted.
func ContractEvents(svc *app.Service, log *slog.Logger) kafkax.Handler {
	return func(ctx context.Context, e kafkax.Envelope, claim kafkax.Claim) error {
		// Kafka work has no logged-in browser user, but every downstream gRPC
		// call still needs a signed tenant identity. Without this, supplier and
		// numbering lookups are rejected as unauthenticated and the contract is
		// parked without a purchase order.
		ctx = grpcx.WithOperator(ctx, grpcx.Operator{TenantID: e.TenantID, Name: "procurement contract consumer"})
		if e.EventType == eventQuotationRejected {
			var rejected app.QuotationRejected
			if err := json.Unmarshal(e.Payload, &rejected); err != nil {
				log.Error("rejected quotation event: unreadable payload, skipping", "event_id", e.EventID, "err", err)
				return nil
			}
			return svc.ReturnRejectedQuotationToCosting(ctx, e.TenantID, rejected, log, app.EventClaim(claim))
		}
		if e.EventType == eventQuotationAccepted {
			var accepted app.QuotationAccepted
			if err := json.Unmarshal(e.Payload, &accepted); err != nil {
				log.Error("quotation event: unreadable payload, skipping", "event_id", e.EventID, "err", err)
				return nil
			}
			if accepted.QuotationID == 0 {
				log.Error("quotation event without quotation id, skipping", "event_id", e.EventID)
				return nil
			}
			return svc.RequirementsFromAcceptedQuotation(ctx, e.TenantID, accepted, log, app.EventClaim(claim))
		}
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
		return svc.RequirementsFromContract(ctx, e.TenantID, c, log, app.EventClaim(claim))
	}
}
