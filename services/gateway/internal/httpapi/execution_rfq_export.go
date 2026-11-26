package httpapi

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/xlsx"
)

type executionRFQExportLine struct {
	RequirementID         string `json:"requirement_id"`
	RequiredQty           string `json:"required_qty"`
	ProductName           string `json:"product_name"`
	MaterialStandard      string `json:"material_standard"`
	Specification         string `json:"specification"`
	TechnicalRequirements string `json:"technical_requirements"`
}

func (s *Server) exportExecutionRFQ(w http.ResponseWriter, r *http.Request) {
	var input struct {
		BatchKey string                   `json:"batch_key"`
		Lines    []executionRFQExportLine `json:"lines"`
	}
	if !s.decodeJSON(w, r, &input) {
		return
	}
	batchKey := strings.TrimSpace(input.BatchKey)
	if batchKey == "" || len(input.Lines) == 0 || len(input.Lines) >= xlsx.MaxRows {
		s.writeError(w, http.StatusBadRequest, "EXECUTION_RFQ_EXPORT_LINES", "请选择包含 1 至 999 条采购规格的实单")
		return
	}
	batch, err := s.Requirements.ListRequirements(r.Context(), &prv1.ListRequirementsRequest{
		ExecutionBatchView: true, ExecutionBatchKey: batchKey,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if len(batch.GetRequirements()) == 0 {
		s.writeError(w, http.StatusNotFound, "EXECUTION_RFQ_EXPORT_BATCH", "找不到可查看的实单")
		return
	}
	required := make(map[string]*prv1.Requirement)
	for _, requirement := range batch.GetRequirements() {
		if qty, err := strconv.ParseFloat(requirement.GetRequiredQty(), 64); err == nil && qty > 0 {
			required[strconv.FormatInt(requirement.GetId(), 10)] = requirement
		}
	}
	if len(required) != len(input.Lines) {
		s.writeError(w, http.StatusConflict, "EXECUTION_RFQ_EXPORT_CHANGED", "采购规格已变化，请刷新实单后重新导出")
		return
	}
	for _, line := range input.Lines {
		requirement := required[line.RequirementID]
		if requirement == nil || requirement.GetRequiredQty() != line.RequiredQty {
			s.writeError(w, http.StatusConflict, "EXECUTION_RFQ_EXPORT_CHANGED", "采购规格或数量已变化，请刷新实单后重新导出")
			return
		}
		delete(required, line.RequirementID)
		if strings.TrimSpace(line.ProductName) == "" {
			s.writeError(w, http.StatusBadRequest, "EXECUTION_RFQ_EXPORT_PRODUCT", "请填写每条规格的产品名称")
			return
		}
	}
	data, err := buildExecutionRFQWorkbook(input.Lines, batch.GetRequirements())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "EXECUTION_RFQ_EXPORT_FAILED", "生成询价 Excel 失败")
		return
	}
	writeXLSXDownload(w, "工厂询价明细.xlsx", data)
}

func buildExecutionRFQWorkbook(lines []executionRFQExportLine, requirements []*prv1.Requirement) ([]byte, error) {
	units := make(map[string]string, len(requirements))
	for _, requirement := range requirements {
		units[strconv.FormatInt(requirement.GetId(), 10)] = requirement.GetUomCode()
	}
	type column struct {
		name    string
		width   int
		numeric bool
		value   func(executionRFQExportLine) string
	}
	columns := []column{
		{"产品名称", 28, false, func(line executionRFQExportLine) string { return line.ProductName }},
		{"材质/标准", 25, false, func(line executionRFQExportLine) string { return line.MaterialStandard }},
		{"规格", 68, false, func(line executionRFQExportLine) string { return line.Specification }},
		{"采购需求量", 14, true, func(line executionRFQExportLine) string { return line.RequiredQty }},
		{"单位", 10, false, func(line executionRFQExportLine) string { return units[line.RequirementID] }},
		{"备注", 66, false, func(line executionRFQExportLine) string { return line.TechnicalRequirements }},
	}
	columns = slices.DeleteFunc(columns, func(column column) bool {
		return !slices.ContainsFunc(lines, func(line executionRFQExportLine) bool {
			return strings.TrimSpace(column.value(line)) != ""
		})
	})
	rows := make([][]string, 1, len(lines)+1)
	widths := make([]int, len(columns))
	rows[0] = make([]string, len(columns))
	for index, column := range columns {
		rows[0][index], widths[index] = column.name, column.width
	}
	numbers := make(map[string]string, len(lines))
	for index, line := range lines {
		row := make([]string, len(columns))
		for columnIndex, column := range columns {
			value := strings.TrimSpace(column.value(line))
			if column.numeric {
				numbers[string(rune('A'+columnIndex))+strconv.Itoa(index+2)] = value
			} else {
				row[columnIndex] = value
			}
		}
		rows = append(rows, row)
	}
	return xlsx.BuildWrapped("工厂询价明细", rows, numbers, widths)
}
