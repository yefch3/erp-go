package app

import "testing"

func TestOnlyOfficeFilesAreOfferedAConvertedPreview(t *testing.T) {
	yes := []string{
		"报价单.docx", "quotation.DOC", "list.xlsx", "LIST.XLS",
		"deck.pptx", "deck.ppt", "note.rtf", "a.odt", "b.ods", "c.odp",
		// 客户发来的文件名常常带空格和多个点。
		" 2026 报价 v2.final.xlsx ",
	}
	for _, n := range yes {
		if !convertibleToPDF(n) {
			t.Errorf("%q 应该能转", n)
		}
	}
	no := []string{
		// 已经能直接看的，不该多绕一趟 LibreOffice。
		"photo.jpg", "scan.pdf", "logo.png",
		// 转了也没意义、或者根本不知道是什么。
		"archive.zip", "data.csv", "readme.txt", "attachment", "", "   ",
		// 扩展名像但不是：不能只看结尾几个字。
		"notadoc", "xlsx", "report.docx.exe",
	}
	for _, n := range no {
		if convertibleToPDF(n) {
			t.Errorf("%q 不该被送去转换", n)
		}
	}
}

func TestPreviewKeyIsDerivedFromTheOriginalAndIsStable(t *testing.T) {
	k1 := previewKeyFor("mail/7/att/abc123")
	k2 := previewKeyFor("mail/7/att/abc123")
	if k1 != k2 {
		t.Fatalf("同一个原件要推出同一个键：%q vs %q", k1, k2)
	}
	if k1 == "" || k1 == "mail/7/att/abc123" {
		t.Fatalf("键要和原件区分开：%q", k1)
	}
	if previewKeyFor("mail/7/att/other") == k1 {
		t.Fatal("不同原件不能撞同一个键")
	}
	// 没有原件就没有预览，别编一个键出来。
	if previewKeyFor("") != "" || previewKeyFor("   ") != "" {
		t.Fatal("空的原件键应该返回空")
	}
}
