package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

// 附件报的类型笼统时按扩展名补；报了具体类型的不动。
func TestPreviewTypeFallsBackToTheExtensionOnlyWhenTheDeclaredTypeIsVague(t *testing.T) {
	cases := []struct {
		declared, file string
		previewable    bool
		why            string
	}{
		// 这次的起因：同一份 PDF，标签不同，一个能看一个不能。
		{"application/pdf", "AAA INDUSTRY - Presentation.pdf", true, "标了 PDF 的本来就能看"},
		{"application/octet-stream", "AAA INDUSTRY - Presentation.pdf", true, "标签笼统，名字是 .pdf：该能看"},
		{"application/octet-stream; name=\"x.pdf\"", "x.pdf", true, "带参数的笼统标签也一样"},
		{"", "scan.PDF", true, "没有标签、扩展名大写"},
		{"binary/octet-stream", "photo.jpg", true, "别的笼统写法、图片"},
		{"application/x-download", "logo.png", true, ""},
		{"APPLICATION/OCTET-STREAM", "chart.webp", true, "大小写不敏感"},
		// 报了具体类型的，说的是另一回事——不按名字猜。
		{"message/rfc822", "invoice.pdf", false, "转发的整封信，名字碰巧是 .pdf"},
		{"application/applefile", "photo.jpeg", false, "苹果的资源分叉，不是图片本身"},
		{"text/html", "report.pdf", false, "报了网页就不当 PDF"},
		// 笼统，但名字也说不出能预览的类型。
		{"application/octet-stream", "quote.docx", false, "Word 走在线 Office 那条路，不在这里"},
		{"application/octet-stream", "archive.zip", false, ""},
		{"application/octet-stream", "noextension", false, ""},
		{"application/octet-stream", "drawing.svg", false, "SVG 能带脚本，不在白名单里"},
	}
	for _, c := range cases {
		got := previewable(previewTypeOf(c.declared, c.file)) != ""
		if got != c.previewable {
			t.Errorf("previewTypeOf(%q, %q) 能否预览 = %v，想要 %v（%s）", c.declared, c.file, got, c.previewable, c.why)
		}
	}
}

// 整条路：标签笼统的 PDF 拿到预览按钮，预览链接按 PDF 签（浏览器才会用 PDF
// 查看器打开），下载链接不受影响。
func TestAVaguelyLabelledPDFGetsAPreviewSignedAsPDF(t *testing.T) {
	s := &Service{
		files: &previewStore{objects: map[string][]byte{}},
		log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	atts := s.signDownloads(context.Background(), []Attachment{{
		FileName: "AAA INDUSTRY - Presentation.pdf", ContentType: "application/octet-stream",
		FileKey: "mail/inbound/4/42/att.pdf",
	}})
	a := atts[0]
	if a.PreviewKind != PreviewDirect {
		t.Fatalf("应该能预览，PreviewKind=%q", a.PreviewKind)
	}
	if !strings.Contains(a.PreviewURL, "ct=application/pdf") {
		t.Fatalf("预览链接应该按 PDF 签：%s", a.PreviewURL)
	}
	if a.DownloadURL == "" {
		t.Fatal("下载链接不该受影响")
	}
	if a.ContentType != "application/octet-stream" {
		t.Fatalf("附件自己报的类型不改写：%q", a.ContentType)
	}
}
