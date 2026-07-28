// Package kafkain holds export's event consumers. This is the read side of
// the asynchronous contract with other services: nothing here calls back into
// the producer, everything it needs is in the payload.
package kafkain

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/kafkax"
	"github.com/sgao19/erp-go/services/export/internal/app"
)

// decision mirrors what the approval service publishes. Only the fields
// export actually acts on are declared; the rest is ignored on purpose, so a
// new field upstream does not break this consumer.
type decision struct {
	InstanceID int64  `json:"instance_id"`
	BizType    string `json:"biz_type"`
	BizID      int64  `json:"biz_id"`
	BizNo      string `json:"biz_no"`
	Result     string `json:"result"`
}

// ApprovalDecisions advances contracts when the approval engine finishes with
// them. Approval never learns what a contract is; it says "1234 was
// approved", and this is where that becomes a state change.
func ApprovalDecisions(svc *app.Service, log *slog.Logger) kafkax.Handler {
	return func(ctx context.Context, e kafkax.Envelope) error {
		var d decision
		if err := json.Unmarshal(e.Payload, &d); err != nil {
			// Retrying cannot fix a payload we cannot read.
			log.Error("approval event: unreadable payload, skipping",
				"event_id", e.EventID, "err", err)
			return nil
		}
		if d.BizType != app.BizTypeContract {
			return nil // some other document type; not ours to handle
		}

		status, err := svc.ApplyApprovalDecision(ctx, e.TenantID, d.BizID, d.Result)
		if err == nil {
			log.Info("contract advanced by approval",
				"contract_id", d.BizID, "contract_no", d.BizNo,
				"result", d.Result, "status", status)
			return nil
		}
		// A decision about a contract that does not exist is a dead letter,
		// not a transient failure: the approval backlog still holds test
		// instances whose biz_ids were never real documents. Returning an
		// error would park the consumer group on them forever.
		if apierr.CodeFromError(err) == "EX_CONTRACT_NOT_FOUND" {
			log.Warn("approval event references an unknown contract, skipping",
				"biz_id", d.BizID, "biz_no", d.BizNo, "instance_id", d.InstanceID)
			return nil
		}
		return err
	}
}
