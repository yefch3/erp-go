package kafkain

import (
	"context"
	"encoding/json"
	"github.com/sgao19/erp-go/pkg/kafkax"
	"github.com/sgao19/erp-go/services/shipping/internal/app"
	"log/slog"
)

type approvalDecision struct {
	InstanceID int64  `json:"instance_id"`
	BizType    string `json:"biz_type"`
	BizID      int64  `json:"biz_id"`
	Result     string `json:"result"`
	Comment    string `json:"comment"`
}

func ApprovalDecisions(s *app.Service, log *slog.Logger) kafkax.Handler {
	return func(ctx context.Context, e kafkax.Envelope, claim kafkax.Claim) error {
		var d approvalDecision
		if err := json.Unmarshal(e.Payload, &d); err != nil {
			return nil
		}
		if d.BizType != app.BizTypeShippingRequote {
			return nil
		}
		return s.ApplyRequoteApproval(ctx, e.TenantID, d.BizID, d.InstanceID, d.Result, d.Comment, app.EventClaim(claim))
	}
}
