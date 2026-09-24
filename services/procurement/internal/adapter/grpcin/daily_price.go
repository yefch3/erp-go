package grpcin

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

type DailyPriceHandler struct {
	prv1.UnimplementedDailyPriceServiceServer
	svc *app.Service
}

func NewDailyPrice(svc *app.Service) *DailyPriceHandler { return &DailyPriceHandler{svc: svc} }

func (h *DailyPriceHandler) Execute(ctx context.Context, r *prv1.DailyPriceServiceExecuteRequest) (*prv1.DailyPriceServiceExecuteResponse, error) {
	if len(r.GetCommandJson()) > 1024*1024 {
		return nil, apierr.Invalid("DAILY_PRICE_SIZE", "一次提交的数据过多")
	}
	var in app.DailyPriceCommand
	d := json.NewDecoder(strings.NewReader(r.GetCommandJson()))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return nil, apierr.Invalid("DAILY_PRICE_INPUT", "每日基价输入无效")
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, apierr.Invalid("DAILY_PRICE_INPUT", "每日基价输入无效")
	}
	op, ok := grpcx.OperatorFromContext(ctx)
	if !ok {
		return nil, apierr.Unauthorized("DAILY_PRICE_IDENTITY", "请先登录")
	}
	result, err := h.svc.DailyPrice(ctx, grpcx.TenantID(ctx), app.Operator{ID: op.EmployeeID, Name: op.Name}, in)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &prv1.DailyPriceServiceExecuteResponse{ResultJson: string(data)}, nil
}
