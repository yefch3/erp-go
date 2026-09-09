package app

import (
	"strings"
	"testing"
)

// 编辑器粘表格时生成的那段 HTML，必须原样活着穿过发信白名单。
//
// 这条测试存在的理由：白名单在服务端，生成表格的代码在前端，两边谁都不知道
// 对方。中间任何一处不合（少一个标签、某条 CSS 被 bluemonday 判定不合法）
// 的表现都是「编辑器里好看、收件人收到的没有框线」——而那种错只有真发一封
// 才看得见。前端那份见 frontend/src/lib/pastedTable.ts。
func TestPastedTableSurvivesOutgoingPolicy(t *testing.T) {
	const cell = "border:1px solid #d0d0d0;padding:4px 8px;"
	in := `<table style="border-collapse:collapse;width:100%;">` +
		`<thead><tr><th style="` + cell + `">品名</th><th style="` + cell + `">数量</th></tr></thead>` +
		`<tbody><tr><td style="` + cell + `">钢卷</td><td style="` + cell + `">100</td></tr></tbody></table>`

	out := SanitizeHTML(in)

	for _, want := range []string{
		"<table", "<thead", "<tbody", "<th", "<td", "品名", "钢卷",
		// 框线和内边距是这个表格能读的全部理由。少一条都算坏。
		"border-collapse", "border:1px solid", "padding:4px 8px",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("发信白名单吃掉了 %q\n实际：%s", want, out)
		}
	}
}

// 图片那一半：宽度按真实尺寸走之后，width 属性和 max-width/height 样式都要留住。
func TestInsertedImageSurvivesOutgoingPolicy(t *testing.T) {
	in := `<img src="https://erp.example.com/i/abc" alt="报价单" width="600" style="max-width:100%;height:auto">`
	out := SanitizeHTML(in)
	for _, want := range []string{`width="600"`, "max-width", "height:auto", `alt="报价单"`} {
		if !strings.Contains(out, want) {
			t.Errorf("发信白名单吃掉了 %q\n实际：%s", want, out)
		}
	}
}
