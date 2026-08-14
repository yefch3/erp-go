package kafkain

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/kafkax"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

// decision mirrors what the approval service publishes. Only the fields
// procurement acts on are declared, so a new field upstream does not break
// this consumer.
type decision struct {
	InstanceID int64  `json:"instance_id"`
	BizType    string `json:"biz_type"`
	BizID      int64  `json:"biz_id"`
	BizNo      string `json:"biz_no"`
	Result     string `json:"result"`
	Comment    string `json:"comment"`
}

// ApprovalDecisions turns "1234 was approved" into a purchase order that is
// actually placed. Approval never learns what a purchase order is; this is
// where its verdict becomes a commitment to a supplier and the requirements
// behind it finally count as ordered.
func ApprovalDecisions(svc *app.Service, log *slog.Logger) kafkax.Handler {
	return func(ctx context.Context, e kafkax.Envelope) error {
		var d decision
		if err := json.Unmarshal(e.Payload, &d); err != nil {
			log.Error("approval event: unreadable payload, skipping",
				"event_id", e.EventID, "err", err)
			return nil
		}
		if d.BizType != app.BizTypePurchaseOrder {
			return nil // some other document type; not ours
		}
		status, err := svc.ApplyApprovalDecision(
			ctx, e.TenantID, d.BizID, d.InstanceID, d.Result, d.Comment,
		)
		if err == nil {
			log.Info("purchase order advanced by approval",
				"po_id", d.BizID, "po_no", d.BizNo, "result", d.Result, "status", status)
			return nil
		}
		// A decision about an order that does not exist is a dead letter, not
		// a transient failure. Returning an error would park the consumer
		// group on it forever.
		if apierr.CodeFromError(err) == "PO_ORDER_NOT_FOUND" {
			log.Warn("approval event references an unknown purchase order, skipping",
				"biz_id", d.BizID, "biz_no", d.BizNo, "instance_id", d.InstanceID)
			return nil
		}
		return err
	}
}
