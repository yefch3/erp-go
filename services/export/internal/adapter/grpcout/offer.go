package grpcout

import (
	"context"
	"encoding/json"
	fxv1 "github.com/sgao19/erp-go/gen/go/erp/fx/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/export/internal/app"
	"google.golang.org/grpc"
	"strconv"
)

type OfferSource struct{ client prv1.SourcingServiceClient }

func NewOfferSource(conn *grpc.ClientConn) *OfferSource {
	return &OfferSource{client: prv1.NewSourcingServiceClient(conn)}
}
func (s *OfferSource) ReadInquiry(ctx context.Context, id int64) (app.OfferInquiry, error) {
	command, _ := json.Marshal(map[string]string{"action": "get", "view": "QUOTATIONS", "id": strconv.FormatInt(id, 10)})
	r, err := s.client.InquiryWorkspace(ctx, &prv1.InquiryWorkspaceRequest{CommandJson: string(command)}, grpc.MaxCallRecvMsgSize(16*1024*1024))
	if err != nil {
		return app.OfferInquiry{}, err
	}
	var response struct {
		Item *app.OfferInquiry `json:"item"`
	}
	if err = json.Unmarshal([]byte(r.GetResultJson()), &response); err != nil {
		return app.OfferInquiry{}, err
	}
	if response.Item == nil {
		return app.OfferInquiry{}, apierr.NotFound("OFFER_INQUIRY", "询盘不存在")
	}
	return *response.Item, nil
}
func (s *Scopes) HasPermission(ctx context.Context, id int64, code string) (bool, error) {
	r, err := s.client.CheckPermission(ctx, &iamv1.CheckPermissionRequest{EmployeeId: id, PermissionCode: code})
	if err != nil {
		return false, err
	}
	return r.GetAllowed(), nil
}
func (r *Rates) EffectiveRates(ctx context.Context) ([]app.OfferRate, error) {
	result, err := r.client.ListEffectiveRates(ctx, &fxv1.ListEffectiveRatesRequest{})
	if err != nil {
		return nil, err
	}
	out := []app.OfferRate{}
	for _, rate := range result.GetRates() {
		if rate.GetRate() != "" {
			out = append(out, app.OfferRate{Base: rate.GetBaseCurrency(), Quote: rate.GetQuoteCurrency(), Value: rate.GetRate(), At: rate.GetConfirmedAt()})
		}
	}
	return out, nil
}
