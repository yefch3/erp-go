package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// excelJobFixtureWorkbook 是前端测试解的那份文件的内容：三行询盘，第二行
// 有单价（总价列落公式），产品名里带 XML 要转义的字符。
func excelJobFixtureWorkbook() Workbook {
	return NewTemplateWorkbook(ExtractedInquiry{
		Title: "询盘", Summary: "三条询盘，一条带单价",
		Items: []map[string]string{
			{"product": "热轧钢卷", "quantity": "100", "quantity_unit": "TON"},
			{"product": "冷轧钢卷 <A&B>", "quantity": "50", "quantity_unit": "TON", "unit_price": "620.5"},
			{"product": "镀锌卷", "quantity_unit": "TON"},
		},
	}, SystemInquiryColumns())
}

func TestWorkbookWithoutRowsKeepsOnlyMetadata(t *testing.T) {
	book := excelJobFixtureWorkbook()
	meta := book.withoutRows()

	sheet, orig := meta.Sheets[0], book.Sheets[0]
	if sheet.Rows != nil || sheet.PreviewRows != nil {
		t.Fatalf("落库的那份不该带行：rows=%d preview_rows=%d", len(sheet.Rows), len(sheet.PreviewRows))
	}
	// 原来那份接着要拿去生成文件，一格都不能少。
	if len(orig.Rows) != 3 || len(orig.PreviewRows) != 3 {
		t.Fatalf("原 Workbook 被动了：rows=%d preview_rows=%d", len(orig.Rows), len(orig.PreviewRows))
	}
	// 文件里没有的那几样，一样都不能丢。
	if sheet.Name != orig.Name || sheet.Summary != orig.Summary ||
		!slices.Equal(sheet.Columns, orig.Columns) ||
		!slices.Equal(sheet.ColumnTypes, orig.ColumnTypes) ||
		!slices.Equal(sheet.ColumnKeys, orig.ColumnKeys) {
		t.Fatalf("metadata 少了东西：%+v", sheet)
	}
	if meta.Title != book.Title {
		t.Fatalf("title = %q, want %q", meta.Title, book.Title)
	}

	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"rows"`) || strings.Contains(string(raw), `"preview_rows"`) {
		t.Fatalf("落库的 JSON 里还有行：%s", raw)
	}
	for _, key := range []string{`"summary"`, `"columns"`, `"column_types"`, `"column_keys"`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("落库的 JSON 缺 %s：%s", key, raw)
		}
	}
}

// 改动前完成的旧任务 workbook_json 里还带着行，过了留存期才清。两种都要
// 读得出来，而且旧任务的行要照发——它们的文件里公式格的缓存值是 0（那
// 时还没修），从文件里解会把总价显示成 0。
func TestExcelJobFromRowReadsLegacyAndMetadataOnlyWorkbooks(t *testing.T) {
	book := excelJobFixtureWorkbook()
	legacy, err := json.Marshal(book)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(book.withoutRows())
	if err != nil {
		t.Fatal(err)
	}
	row := func(workbook []byte) store.MailExcelJob {
		return store.MailExcelJob{
			Status: "COMPLETED", FileName: "询盘.xlsx", Model: "test",
			WorkbookJson: workbook, TemplateColumns: []byte(`[]`),
		}
	}

	got, err := excelJobFromRow(row(legacy))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got.Result.Workbook.Sheets[0].PreviewRows); n != 3 {
		t.Fatalf("旧任务的行要照发，实际 %d 行", n)
	}

	got, err = excelJobFromRow(row(meta))
	if err != nil {
		t.Fatal(err)
	}
	sheet := got.Result.Workbook.Sheets[0]
	if len(sheet.Rows) != 0 || len(sheet.PreviewRows) != 0 {
		t.Fatalf("metadata-only 的任务不该读出行：%+v", sheet)
	}
	if sheet.Summary == "" || len(sheet.ColumnKeys) != len(sheet.Columns) || len(sheet.Columns) == 0 {
		t.Fatalf("metadata 读丢了：%+v", sheet)
	}
	if got.Result.FileName != "询盘.xlsx" {
		t.Fatalf("file name = %q", got.Result.FileName)
	}
}

// 前端解的样本文件：frontend/src/lib/testdata/excel-job-result.xlsx。
//
// 预览的行从这份文件里读，所以「Go 写出来的」和「浏览器读得懂的」必须是
// 同一份东西。这条测试钉住前一半：样本文件就是现在的写法写出来的——写文件
// 的代码一变（比如哪天不再在公式旁边存算好的值），这里先红，而不是等到
// 线上预览总价变成 0。
//
// 比的是解压之后每个成员的内容，不比字节：压缩结果随 Go 版本变，内容不变。
//
// 重新生成：EXCEL_FIXTURE_WRITE=1 go test -run TestExcelJobFixtureMatchesWriter ./internal/app
const excelJobFixturePath = "../../../../frontend/src/lib/testdata/excel-job-result.xlsx"

func TestExcelJobFixtureMatchesWriter(t *testing.T) {
	fresh, err := buildXLSX(excelJobFixtureWorkbook())
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("EXCEL_FIXTURE_WRITE") != "" {
		if err := os.MkdirAll(filepath.Dir(excelJobFixturePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(excelJobFixturePath, fresh, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := os.ReadFile(excelJobFixturePath)
	if err != nil {
		t.Fatalf("读不到前端的样本文件（EXCEL_FIXTURE_WRITE=1 重新生成）：%v", err)
	}
	want, got := unzipMembers(t, fresh), unzipMembers(t, stored)
	if !maps.Equal(want, got) {
		t.Fatalf("样本文件和现在写出来的不一样；改了写文件的代码就要重新生成：" +
			"EXCEL_FIXTURE_WRITE=1 go test -run TestExcelJobFixtureMatchesWriter ./internal/app")
	}
	// 前端那条测试赖以成立的前提，在这里也说一遍：公式格旁边存着算好的值。
	if !strings.Contains(got["xl/worksheets/sheet1.xml"], `<f>T3*U3</f><v>31025</v>`) {
		t.Fatalf("公式格没带算好的值：%s", got["xl/worksheets/sheet1.xml"])
	}
}

func unzipMembers(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(b)
	}
	return out
}
