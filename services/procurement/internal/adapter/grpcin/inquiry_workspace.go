package grpcin

import (
	"context"
	"encoding/json"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
	"io"
	"strings"
)

func (h *SourcingHandler) InquiryWorkspace(ctx context.Context, r *prv1.InquiryWorkspaceRequest) (*prv1.InquiryWorkspaceResponse, error) {
	var in app.InquiryCommand
	// An 8 MB attachment becomes about 10.7 MB after JSON base64 encoding.
	// Keep this aligned with the gateway's 12 MB request budget.
	if len(r.GetCommandJson()) > 12*1024*1024 {
		return nil, apierr.Invalid("INQUIRY_SIZE", "询盘内容过大")
	}
	d := json.NewDecoder(strings.NewReader(r.GetCommandJson()))
	d.DisallowUnknownFields()
	if e := d.Decode(&in); e != nil {
		return nil, apierr.Invalid("INQUIRY_INPUT", "询盘输入无效")
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, apierr.Invalid("INQUIRY_INPUT", "询盘输入无效")
	}
	result, e := h.svc.InquiryWorkspace(ctx, grpcx.TenantID(ctx), sourcingOperator(ctx), in)
	if e != nil {
		return nil, e
	}
	data, e := json.Marshal(result)
	if e != nil {
		return nil, e
	}
	return &prv1.InquiryWorkspaceResponse{ResultJson: string(data)}, nil
}
