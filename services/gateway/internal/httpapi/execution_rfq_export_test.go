package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/xlsx"
	"google.golang.org/grpc"
)

type executionRFQExportRequirementsStub struct {
	prv1.RequirementServiceClient
}

func (executionRFQExportRequirementsStub) ListRequirements(_ context.Context, req *prv1.ListRequirementsRequest, _ ...grpc.CallOption) (*prv1.ListRequirementsResponse, error) {
	if !req.GetExecutionBatchView() || req.GetExecutionBatchKey() != "CONTRACT-ID:321" {
		return &prv1.ListRequirementsResponse{}, nil
	}
	return &prv1.ListRequirementsResponse{Requirements: []*prv1.Requirement{
		{Id: 101, UomCode: "MT", AvailableQty: "17.125", CustomerName: "PRIVATE CUSTOMER",
			ContractNo: "PRIVATE CONTRACT", SourceUnitPrice: "98321.56", SourceSalesRemark: "PRIVATE SALES NOTE",
			ContractItemRemark: "PRIVATE CONTRACT NOTE", PresaleReferenceJson: "PRIVATE PRESALE QUOTE"},
		{Id: 102, UomCode: "PCS", AvailableQty: "25"},
		{Id: 103, UomCode: "MT", AvailableQty: "0"},
	}}, nil
}

func TestExecutionRFQExportContainsOnlyReviewedFields(t *testing.T) {
	s := &Server{Requirements: executionRFQExportRequirementsStub{}}
	longSpec := strings.Repeat("耐蚀冷轧板，厚度及公差按图纸要求。", 14)
	body := `{"batch_key":"CONTRACT-ID:321","lines":[
		{"requirement_id":"101","available_qty":"17.125","product_name":"钢卷","material_standard":"JIS G3141","specification":"` + longSpec + `","technical_requirements":"防潮包装\n附检验报告"},
		{"requirement_id":"102","available_qty":"25","product_name":"钢卷","specification":"2.0 × 1250 mm","technical_requirements":"=LITERAL"}
	]}`
	rec := httptest.NewRecorder()
	s.exportExecutionRFQ(rec, httptest.NewRequest(http.MethodPost, "/api/requirements/execution-rfq/export", strings.NewReader(body)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), ".xlsx") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	rows, err := xlsx.Parse(rec.Body.Bytes())
	want := [][]string{
		{"产品名称", "材质/标准", "规格", "采购数量", "单位", "备注"},
		{"钢卷", "JIS G3141", longSpec, "17.125", "MT", "防潮包装\n附检验报告"},
		{"钢卷", "", "2.0 × 1250 mm", "25", "PCS", "=LITERAL"},
	}
	if err != nil || !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %#v, error %v", rows, err)
	}
	data := rec.Body.Bytes()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range archive.File {
		file, err := part.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		xml := string(content)
		if strings.Contains(xml, "PRIVATE") || strings.Contains(xml, "98321.56") {
			t.Fatalf("internal data found in %s", part.Name)
		}
		switch part.Name {
		case "xl/worksheets/sheet1.xml":
			for _, entry := range []string{`width="68"`, `width="66"`, `<row r="2" ht="`, `<c r="D2" s="4"><v>17.125</v>`, `<c r="D3" s="5"><v>25</v>`, `<c r="F3" t="inlineStr" s="3">`} {
				if !strings.Contains(xml, entry) {
					t.Errorf("missing workbook layout or value %s", entry)
				}
			}
		case "xl/styles.xml":
			if !strings.Contains(xml, `wrapText="1"`) {
				t.Error("workbook does not enable text wrapping")
			}
		}
	}
}

func TestExecutionRFQExportRejectsAnUnavailableOrChangedBatch(t *testing.T) {
	s := &Server{Requirements: executionRFQExportRequirementsStub{}}
	for _, tc := range []struct {
		name, key, lines string
		status           int
	}{
		{"outside scope", "CONTRACT-ID:999", `{"requirement_id":"101"}`, http.StatusNotFound},
		{"incomplete", "CONTRACT-ID:321", `{"requirement_id":"101","available_qty":"17.125"}`, http.StatusConflict},
		{"changed quantity", "CONTRACT-ID:321", `{"requirement_id":"101","available_qty":"17","product_name":"钢卷"},{"requirement_id":"102","available_qty":"25","product_name":"钢卷"}`, http.StatusConflict},
		{"line from another batch", "CONTRACT-ID:321", `{"requirement_id":"101","available_qty":"17.125","product_name":"钢卷"},{"requirement_id":"777","available_qty":"25","product_name":"钢卷"}`, http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			body := `{"batch_key":"` + tc.key + `","lines":[` + tc.lines + `]}`
			s.exportExecutionRFQ(rec, httptest.NewRequest(http.MethodPost, "/api/requirements/execution-rfq/export", strings.NewReader(body)))
			if rec.Code != tc.status {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestExecutionRFQExportOmitsOnlyColumnsWithoutAnyValues(t *testing.T) {
	lines := []executionRFQExportLine{
		{RequirementID: "101", AvailableQty: "17.125", ProductName: "钢卷", MaterialStandard: " ", TechnicalRequirements: " \n "},
		{RequirementID: "102", AvailableQty: "25", ProductName: "方管", Specification: "30 × 30 mm"},
	}
	requirements := []*prv1.Requirement{{Id: 101, UomCode: "MT"}, {Id: 102, UomCode: "PCS"}}
	for _, tc := range []struct {
		name string
		note string
		want [][]string
	}{
		{
			name: "all remarks blank",
			want: [][]string{{"产品名称", "规格", "采购数量", "单位"}, {"钢卷", "", "17.125", "MT"}, {"方管", "30 × 30 mm", "25", "PCS"}},
		},
		{
			name: "one reviewed remark",
			note: "独立包装",
			want: [][]string{{"产品名称", "规格", "采购数量", "单位", "备注"}, {"钢卷", "", "17.125", "MT"}, {"方管", "30 × 30 mm", "25", "PCS", "独立包装"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines[1].TechnicalRequirements = tc.note
			data, err := buildExecutionRFQWorkbook(lines, requirements)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := xlsx.Parse(data)
			if err != nil || !reflect.DeepEqual(rows, tc.want) {
				t.Fatalf("rows = %#v, error %v", rows, err)
			}
		})
	}
}
