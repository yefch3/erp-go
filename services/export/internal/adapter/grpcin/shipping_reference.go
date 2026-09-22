package grpcin

import (
	"context"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func (h *ContractHandler) ListShippingContractReferences(ctx context.Context, r *exv1.ListShippingContractReferencesRequest) (*exv1.ListShippingContractReferencesResponse, error) {
	if err := h.svc.RequireAnyPermission(ctx, "shipping:schedule:read", "shipping:schedule:write"); err != nil {
		return nil, err
	}
	rows, err := h.svc.ShippingContractReferences(ctx, grpcx.TenantID(ctx), r.Id, r.Keyword, r.Exact)
	if err != nil {
		return nil, err
	}
	out := &exv1.ListShippingContractReferencesResponse{}
	for _, v := range rows {
		out.Contracts = append(out.Contracts, &exv1.ShippingContractReference{Id: v.ID, ContractNo: v.ContractNo, ExternalContractNo: v.ExternalContractNo})
	}
	return out, nil
}
