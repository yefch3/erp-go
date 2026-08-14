package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

// orderImportPreviewRequest deliberately uses plain JSON instead of a proto:
// the rows are the fixed Excel columns and may contain fields that are still
// being added to the procurement proto. Preview is read-only; confirmation
// later reuses CreateOrder's authoritative validation.
type orderImportPreviewRequest struct {
	Rows []orderImportPreviewRow `json:"rows"`
}

type orderImportPreviewRow struct {
	RowNo            int    `json:"rowNo"`
	Product          string `json:"product"`
	MaterialStandard string `json:"materialStandard"`
	Grade            string `json:"grade"`
	Thickness        string `json:"thickness"`
	Width            string `json:"width"`
	QuantityUnit     string `json:"quantityUnit"`
	Quantity         string `json:"quantity"`
	UnitPrice        string `json:"unitPrice"`
}

type orderImportPreviewResponse struct {
	Rows []orderImportPreviewResult `json:"rows"`
}

type orderImportPreviewResult struct {
	RowNo      int                    `json:"rowNo"`
	Product    string                 `json:"product"`
	Quantity   string                 `json:"quantity"`
	UnitPrice  string                 `json:"unitPrice"`
	Candidates []orderImportCandidate `json:"candidates"`
	Result     string                 `json:"result"`
	Message    string                 `json:"message,omitempty"`
}

type orderImportCandidate struct {
	RequirementID int64  `json:"requirementId"`
	ContractNo    string `json:"contractNo"`
	ProductName   string `json:"productName"`
	ProductCode   string `json:"productCode"`
	Spec          string `json:"spec"`
	UomCode       string `json:"uomCode"`
	RequiredQty   string `json:"requiredQty"`
	OrderedQty    string `json:"orderedQty"`
	Status        string `json:"status"`
}

func (s *Server) previewOrderImport(w http.ResponseWriter, r *http.Request) {
	var in orderImportPreviewRequest
	if !s.decodeJSON(w, r, &in) {
		return
	}
	if len(in.Rows) == 0 {
		s.writeError(w, http.StatusBadRequest, "PO_IMPORT_LINES_REQUIRED", "Excel 至少需要一条产品明细")
		return
	}
	if len(in.Rows) > 200 {
		s.writeError(w, http.StatusBadRequest, "PO_IMPORT_TOO_LARGE", "预览最多处理 200 行，请下载完整模板后再导入")
		return
	}

	out := orderImportPreviewResponse{Rows: make([]orderImportPreviewResult, 0, len(in.Rows))}
	for _, row := range in.Rows {
		result := orderImportPreviewResult{
			RowNo: row.RowNo, Product: row.Product, Quantity: row.Quantity, UnitPrice: row.UnitPrice,
			Candidates: []orderImportCandidate{}, Result: "NOT_FOUND",
		}
		keyword := strings.TrimSpace(row.Product)
		if keyword == "" {
			result.Message = "产品不能为空"
			out.Rows = append(out.Rows, result)
			continue
		}
		resp, err := s.Requirements.ListRequirements(r.Context(), &prv1.ListRequirementsRequest{
			Page: &commonv1.PageRequest{Page: 1, PageSize: 50}, Status: "PENDING", Keyword: keyword,
		})
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}
		for _, candidate := range resp.GetRequirements() {
			result.Candidates = append(result.Candidates, orderImportCandidate{
				RequirementID: candidate.GetId(), ContractNo: candidate.GetContractNo(),
				ProductName: candidate.GetProductName(), ProductCode: candidate.GetProductCode(),
				Spec: candidate.GetSpec(), UomCode: candidate.GetUomCode(),
				RequiredQty: candidate.GetRequiredQty(), OrderedQty: candidate.GetOrderedQty(),
				Status: candidate.GetStatus(),
			})
		}
		switch len(result.Candidates) {
		case 0:
			result.Message = "没有找到待采购需求"
		case 1:
			result.Result = "MATCHED"
		default:
			result.Result = "MULTIPLE"
			result.Message = "找到多个候选，请人工确认"
		}
		out.Rows = append(out.Rows, result)
	}

	data, err := json.Marshal(out)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "GATEWAY_MARSHAL", "响应编码失败")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(envelope{Success: true, Data: data})
}
