package grpcout

import (
	"context"
	"time"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/shipping/internal/app"
	"google.golang.org/grpc"
)

type DeliveryQuality struct {
	client prv1.PurchaseOrderServiceClient
}

func NewDeliveryQuality(conn *grpc.ClientConn) *DeliveryQuality {
	return &DeliveryQuality{client: prv1.NewPurchaseOrderServiceClient(conn)}
}

func (g *DeliveryQuality) Check(ctx context.Context, contractID int64, lines []app.DeliveryQualityLine) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req := &prv1.CheckDeliveryQualityRequest{ContractId: contractID}
	for _, line := range lines {
		req.Lines = append(req.Lines, &prv1.DeliveryQualityContractLine{ContractItemId: line.ContractItemID, Qty: line.Qty})
	}
	result, err := g.client.CheckDeliveryQuality(ctx, req)
	if err != nil {
		return err
	}
	if !result.GetAllowed() {
		code, reason := result.GetCode(), result.GetReason()
		if code == "" {
			code = "QUALITY_DELIVERY_BLOCKED"
		}
		if reason == "" {
			reason = "本批货物尚未全部质检合格，不能交货"
		}
		return apierr.Conflict(code, reason)
	}
	return nil
}
