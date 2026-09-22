package httpapi

import (
	"context"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"net/http"
	"strings"
)

func (s *Server) saveManualShippingOrder(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.SaveManualShippingOrderRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if v := req.Order; v != nil {
		ref, err := s.resolveShippingContract(r.Context(), v.LinkedContractId, v.ContractNo, false)
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}
		if ref != nil {
			v.LinkedContractId = ref.Id
			v.ContractNo = ref.ContractNo
		}

		if v.FinalForwarderId > 0 {
			p, err := s.resolveActiveSupplier(r.Context(), v.FinalForwarderId)
			if err != nil {
				s.writeGRPCError(w, err)
				return
			}
			v.FinalForwarderName = p.GetName()
		}
		if v.ActualCarrierId > 0 {
			p, err := s.resolveActiveSupplier(r.Context(), v.ActualCarrierId)
			if err != nil {
				s.writeGRPCError(w, err)
				return
			}
			v.ActualCarrierName = p.GetName()
		}
	}
	resp, err := s.Shipping.SaveManualShippingOrder(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// Query only number references; never expose the sales contract payload here.
func (s *Server) shippingContractOptions(w http.ResponseWriter, r *http.Request) {
	out, err := s.Contracts.ListShippingContractReferences(r.Context(), &exv1.ListShippingContractReferencesRequest{Keyword: r.URL.Query().Get("keyword")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, out)
}
func (s *Server) resolveShippingContract(ctx context.Context, id int64, number string, required bool) (*exv1.ShippingContractReference, error) {
	number = strings.TrimSpace(number)
	if id < 0 || (id == 0 && number == "") {
		return nil, apierr.Invalid("SHIPPING_CONTRACT_NO_REQUIRED", "请填写或选择合同号")
	}
	req := &exv1.ListShippingContractReferencesRequest{Id: id, Exact: true}
	if id == 0 {
		req.Keyword = number
	}
	out, err := s.Contracts.ListShippingContractReferences(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(out.Contracts) == 1 {
		return out.Contracts[0], nil
	}
	if required || id != 0 {
		return nil, apierr.Invalid("SHIPPING_CONTRACT_MATCH_REQUIRED", "未找到唯一匹配的销售合同，请搜索并选择合同")
	}
	return nil, nil
}
func (s *Server) linkManualShippingContract(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.LinkManualShippingContractRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	if req.ContractId == 0 {
		existing, err := s.Shipping.GetContractShippingHandoff(r.Context(), &shippingv1.GetContractShippingHandoffRequest{Id: req.Id})
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}
		if strings.TrimSpace(req.ContractNo) == "" {
			req.ContractNo = existing.GetHandoff().GetContractNo()
		}
	}
	ref, err := s.resolveShippingContract(r.Context(), req.ContractId, req.ContractNo, true)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	req.ContractId = ref.Id
	req.ContractNo = ref.ContractNo
	out, err := s.Shipping.LinkManualShippingContract(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, out)
}
