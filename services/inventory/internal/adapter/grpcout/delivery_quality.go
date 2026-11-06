package grpcout

import (
	"context"
	"strings"
	"time"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"google.golang.org/grpc"
)

type DeliveryQuality struct {
	client prv1.PurchaseOrderServiceClient
}

func NewDeliveryQuality(conn *grpc.ClientConn) *DeliveryQuality {
	return &DeliveryQuality{client: prv1.NewPurchaseOrderServiceClient(conn)}
}

func (g *DeliveryQuality) Check(ctx context.Context, poIDs []int64) error {
	if len(poIDs) == 0 {
		return apierr.Conflict("IV_QUALITY_SOURCE_REQUIRED", "缺少采购质检来源，不能确认出库")
	}
	// Confirmation holds stock locks while checking quality. Bound a failed
	// dependency so it cannot leave warehouse operations waiting indefinitely.
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := g.client.CheckDeliveryQuality(checkCtx, &prv1.CheckDeliveryQualityRequest{PoIds: poIDs})
	if err != nil {
		return apierr.Conflict("IV_QUALITY_UNAVAILABLE", "暂时无法核验质检结果，请稍后重试，不能确认出库").Wrap(err)
	}
	if !result.GetAllowed() {
		code, reason := strings.TrimSpace(result.GetCode()), strings.TrimSpace(result.GetReason())
		if code == "" {
			code = "IV_QUALITY_NOT_PASSED"
		}
		if reason == "" {
			reason = "货物尚未全部质检合格，不能确认出库"
		}
		return apierr.Conflict(code, reason)
	}
	return nil
}
