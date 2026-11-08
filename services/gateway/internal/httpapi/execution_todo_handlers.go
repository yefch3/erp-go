package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Read access supports reconciliation; it does not make someone an executor.
// Keep the source services' tenant/data scopes and recheck contract execution
// before presenting any outstanding departmental work.
func (s *Server) listExecutionTodos(w http.ResponseWriter, r *http.Request) {
	result := map[string][]json.RawMessage{"finance": {}, "procurement": {}, "shipping": {}}
	appendRow := func(source string, row proto.Message) {
		data, _ := (protojson.MarshalOptions{EmitUnpopulated: true}).Marshal(row)
		result[source] = append(result[source], data)
	}
	allowed := map[string]bool{}

	for _, permission := range []string{"export:receipt:write", "procurement:order:write", "shipping:schedule:write"} {
		yes, err := s.hasPermission(r, permission)
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}
		allowed[permission] = yes
	}
	executable := map[int64]bool{}
	check := func(id int64) (bool, error) {
		if id <= 0 {
			return false, nil
		}
		if value, ok := executable[id]; ok {
			return value, nil
		}
		state, err := s.Contracts.CheckContractExecution(r.Context(), &exv1.CheckContractExecutionRequest{Id: id})
		if status.Code(err) == codes.NotFound {
			executable[id] = false
			return false, nil
		}
		if err != nil {
			return false, err
		}
		executable[id] = state.GetAllowed()
		return state.GetAllowed(), nil
	}
	if allowed["export:receipt:write"] {
		// Finance visibility follows receivables, not customer-master access. Include
		// reconciled records too: accounting closure is not execution approval.
		seen := map[int64]bool{}
		for _, closed := range []bool{false, true} {
			for page := int32(1); ; page++ {
				data, err := s.Receipts.ListReceivableDue(r.Context(), &exv1.ListReceivableDueRequest{ClosedOnly: closed, Page: &commonv1.PageRequest{Page: page, PageSize: 200}})
				if err != nil {
					s.writeGRPCError(w, err)
					return
				}
				for _, row := range data.GetItems() {
					if row.GetExecutionConditionStatus() != "WAITING" || row.GetManuallyEntered() || seen[row.GetContractId()] {
						continue
					}
					seen[row.GetContractId()] = true
					item, _ := json.Marshal(map[string]string{"id": strconv.FormatInt(row.GetContractId(), 10), "contractNo": row.GetContractNo(), "customerName": row.GetCustomerName(), "currency": row.GetCurrency(), "totalAmount": row.GetTotalAmount()})
					result["finance"] = append(result["finance"], item)
				}
				if len(data.GetItems()) == 0 || int64(page)*200 >= data.GetMeta().GetTotal() {
					break
				}
			}
		}
	}
	// Requirement owner is the sales data-scope owner, not the buyer assignee.
	if allowed["procurement:order:write"] {
		for page := int32(1); ; page++ {
			data, err := s.Requirements.ListRequirements(r.Context(), &prv1.ListRequirementsRequest{Status: "WAITING_REQUOTE", Page: &commonv1.PageRequest{Page: page, PageSize: 200}})
			if err != nil {
				s.writeGRPCError(w, err)
				return
			}
			for _, row := range data.GetRequirements() {
				if row.GetStatus() != "WAITING_REQUOTE" {
					continue
				}
				yes, err := check(row.GetContractId())
				if err != nil {
					s.writeGRPCError(w, err)
					return
				}
				if yes {
					appendRow("procurement", row)
				}
			}
			if len(data.GetRequirements()) == 0 || int64(page)*200 >= data.GetMeta().GetTotal() {
				break
			}
		}
	}
	if allowed["shipping:schedule:write"] {
		data, err := s.Shipping.ListContractShippingHandoffs(r.Context(), &shippingv1.ListContractShippingHandoffsRequest{Status: "WAITING_REQUOTE"})
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}
		for _, row := range data.GetHandoffs() {
			if row.GetStatus() != "WAITING_REQUOTE" {
				continue
			}
			yes, err := check(row.GetContractId())
			if err != nil {
				s.writeGRPCError(w, err)
				return
			}
			if yes {
				appendRow("shipping", row)
			}
		}
	}
	s.writeJSON(w, result)
}
