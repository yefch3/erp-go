package grpcin

import (
	"slices"
	"testing"

	"github.com/sgao19/erp-go/services/mail/internal/app"
)

// 2026-09-14 起结果里只有 metadata，行在文件里。每次调用都新造一份：
// 测试里改哪一份都不该影响别的。
func metaOnlyExcelResult() app.ExcelResult {
	return app.ExcelResult{
		FileName: "询盘.xlsx", Data: []byte("xlsx"), Model: "test",
		Workbook: app.Workbook{Sheets: []app.WorkbookSheet{{
			Name: "询盘", Summary: "一条带单价",
			Columns:     []string{"产品", "数量", "单价", "总价"},
			ColumnTypes: []string{"string", "number", "number", "formula"},
			ColumnKeys:  []string{"product", "quantity", "unit_price", "total_price"},
		}}},
	}
}

func TestExcelResultToProtoSendsRowsOnlyForLegacyJobs(t *testing.T) {
	got := excelResultToProto(metaOnlyExcelResult())
	sheet := got.Sheets[0]
	if len(sheet.Rows) != 0 || sheet.TotalRows != 0 {
		t.Fatalf("metadata-only 的表不该带行：rows=%d total=%d", len(sheet.Rows), sheet.TotalRows)
	}
	// 文件里没有的那几样要全到——浏览器从文件里只解得出行和表头。
	if sheet.Summary != "一条带单价" ||
		!slices.Equal(sheet.Columns, []string{"产品", "数量", "单价", "总价"}) ||
		!slices.Equal(sheet.ColumnKeys, []string{"product", "quantity", "unit_price", "total_price"}) {
		t.Fatalf("metadata 没发全：%+v", sheet)
	}
	if string(got.FileData) != "xlsx" || got.FileName != "询盘.xlsx" {
		t.Fatalf("文件没跟着走：%+v", got)
	}

	// 改动前完成、还没过留存期的旧任务：行照发，而且发的是算好的那一版。
	legacy := metaOnlyExcelResult()
	legacy.Workbook.Sheets[0].Rows = [][]string{{"热轧钢卷", "100", "12.5", "=B2*C2"}}
	legacy.Workbook.Sheets[0].PreviewRows = [][]string{{"热轧钢卷", "100", "12.5", "1250"}}
	sheet = excelResultToProto(legacy).Sheets[0]
	if sheet.TotalRows != 1 || len(sheet.Rows) != 1 || sheet.Rows[0].Cells[3] != "1250" {
		t.Fatalf("旧任务的行没照发：%+v", sheet)
	}

	// 更早的旧任务连 preview_rows 都没有：回落到原始行，但算式要擦掉。
	oldest := metaOnlyExcelResult()
	oldest.Workbook.Sheets[0].Rows = [][]string{{"热轧钢卷", "100", "12.5", "=B2*C2"}}
	sheet = excelResultToProto(oldest).Sheets[0]
	if len(sheet.Rows) != 1 || sheet.Rows[0].Cells[3] == "=B2*C2" {
		t.Fatalf("最早的旧任务露出了算式：%+v", sheet)
	}
}
