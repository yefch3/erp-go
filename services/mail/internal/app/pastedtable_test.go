package app

import (
	"strings"
	"testing"
)

// 剪贴板里的 HTML 是不可信输入。这一组钉的是「危险的东西一样都过不去」。
//
// 它比别的测试更值得写死：净化器错了不会报错，只会在某一天变成一次事故，
// 而那一天没人会想起来是这里松的。
func TestPastedTableDropsAnythingDangerous(t *testing.T) {
	cases := map[string]string{
		"脚本": `<table><tr><td><script>alert(1)</script>a</td></tr></table>`,
		"事件处理器": `<table><tr><td onclick="steal()" onmouseover="x()">a</td></tr></table>`,
		"图片（外链会泄露「这封信被打开了」，Word 的是本地路径）": `<table><tr><td><img src="https://tracker.example/x.gif">a</td></tr></table>`,
		"iframe":     `<table><tr><td><iframe src="https://evil.example"></iframe>a</td></tr></table>`,
		"javascript 链接": `<table><tr><td><a href="javascript:alert(1)">a</a></td></tr></table>`,
		"data 链接":       `<table><tr><td><a href="data:text/html,<script>alert(1)</script>">a</a></td></tr></table>`,
		"表单":             `<table><tr><td><form action="https://evil.example"><input name="p"></form>a</td></tr></table>`,
		"style 块":        `<table><tr><td><style>body{display:none}</style>a</td></tr></table>`,
	}
	for name, in := range cases {
		out := CleanPastedTable(in)
		for _, bad := range []string{
			"<script", "onclick", "onmouseover", "<img", "<iframe",
			"javascript:", "data:text/html", "<form", "<input", "<style",
			"alert(", "display:none",
		} {
			if strings.Contains(strings.ToLower(out), strings.ToLower(bad)) {
				t.Errorf("%s：%q 漏过去了\n结果：%s", name, bad, out)
			}
		}
		// 内容本身要留着——净化不是把表格清空。
		if out != "" && !strings.Contains(out, "a") {
			t.Errorf("%s：连内容都没了\n结果：%s", name, out)
		}
	}
}

// Word 和网页的类名、私有属性只对源站点有意义，留着只会让表格走样。
func TestPastedTableDropsSourceSiteJunk(t *testing.T) {
	in := `<table class="MsoNormalTable" id="t1" data-sheets-value="{}" ` +
		`style="mso-border-alt:solid;border-collapse:collapse">` +
		`<tr><td class="xl65" style="mso-pattern:none;font-size:14px">a</td></tr></table>`
	out := CleanPastedTable(in)
	for _, bad := range []string{"class", "id=", "data-sheets", "mso-"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q 漏过去了\n结果：%s", bad, out)
		}
	}
	// 真正想留的那两条要还在。
	for _, want := range []string{"border-collapse", "font-size:14px"} {
		if !hasIgnoringSpaces(out, want) {
			t.Errorf("把有用的 %q 也丢了\n结果：%s", want, out)
		}
	}
}

// 这一版存在的全部理由：那三条纯文本表达不了的东西。
func TestPastedTableKeepsWhatPlainTextCouldNot(t *testing.T) {
	in := `<table style="border-collapse:collapse;width:520px">` +
		`<tr height="40"><th colspan="2" style="font-size:18px;background-color:#eee">合并的表头</th></tr>` +
		`<tr><td width="180" style="font-family:SimSun;font-size:14px">品名</td>` +
		`<td rowspan="2" style="text-align:right;line-height:1.8">数量</td></tr></table>`
	out := CleanPastedTable(in)
	for _, want := range []string{
		`colspan="2"`, `rowspan="2"`, // 合并单元格
		`width="180"`, "width:520px", // 列宽、表宽
		"font-size:18px", "font-size:14px", "font-family:SimSun", // 字号字体
		`height="40"`, "line-height:1.8", // 行高
		"background-color:#eee", "text-align:right", // 底色、对齐
	} {
		if !hasIgnoringSpaces(out, want) {
			t.Errorf("丢了 %q ——这一版就是为了留住它们\n结果：%s", want, out)
		}
	}
}

// Word 把内容裹在 <html><head><style>…</style><body><div> 一层层壳里。
//
// **先摘表格、再净化**，顺序反过来是个经典的坑：bluemonday 丢掉 <style>
// 这个标签，但标签里那段 CSS 是文本节点，会留下来变成正文里一大段乱码。
func TestPastedTableTakesOnlyTheTableOutOfWordsWrapper(t *testing.T) {
	in := `<html xmlns:o="urn:schemas-microsoft-com:office:office"><head>` +
		`<style>p.MsoNormal{margin:0cm;font-size:12.0pt}</style></head>` +
		`<body><div class=WordSection1><p class=MsoNormal>正文前面这句话</p>` +
		`<table><tr><td>表格里的字</td></tr></table></div></body></html>`
	out := CleanPastedTable(in)
	if strings.Contains(out, "MsoNormal") || strings.Contains(out, "margin:0cm") {
		t.Errorf("<style> 里的 CSS 变成正文了\n结果：%s", out)
	}
	if strings.Contains(out, "正文前面这句话") {
		t.Errorf("表格外面的内容也被带进来了\n结果：%s", out)
	}
	if !strings.Contains(out, "表格里的字") {
		t.Errorf("表格内容丢了\n结果：%s", out)
	}
}

// 一行一列、里面只装着另一个表格的，是包装纸不是数据。
func TestPastedTableUnwrapsLayoutTables(t *testing.T) {
	in := `<table><tr><td><table><tr><td>真正的内容</td><td>第二列</td></tr></table></td></tr></table>`
	out := CleanPastedTable(in)
	if n := strings.Count(out, "<table"); n != 1 {
		t.Errorf("外壳表格没剥掉，还有 %d 层\n结果：%s", n, out)
	}
	if !strings.Contains(out, "真正的内容") {
		t.Errorf("剥过头了\n结果：%s", out)
	}
}

func TestPastedTableKeepsRealNestedTables(t *testing.T) {
	// 两行的外层表格不是包装纸，是真的表格，不能剥。
	in := `<table><tr><td><table><tr><td>里</td></tr></table></td></tr><tr><td>外层第二行</td></tr></table>`
	out := CleanPastedTable(in)
	if n := strings.Count(out, "<table"); n != 2 {
		t.Errorf("真的嵌套表格被剥掉了，剩 %d 层\n结果：%s", n, out)
	}
}

// Excel 给的是绝对宽度，一张按 A4 排的表轻松超过 600px 的正文宽度。
func TestPastedTableFitsIntoTheMailBody(t *testing.T) {
	t.Run("超宽的改成百分比", func(t *testing.T) {
		out := CleanPastedTable(`<table style="width:1040px"><tr><td>a</td></tr></table>`)
		if !hasIgnoringSpaces(out, "width:100%") || strings.Contains(out, "1040") {
			t.Errorf("1040px 的表该收进正文宽度\n结果：%s", out)
		}
	})

	t.Run("Word 的 pt 要先换算成 px 再比", func(t *testing.T) {
		// 635pt ≈ 847px，超了。不换算的话会被当成 635px 放过去，然后撑破正文。
		out := CleanPastedTable(`<table style="width:635.25pt"><tr><td>a</td></tr></table>`)
		if !hasIgnoringSpaces(out, "width:100%") {
			t.Errorf("635pt（≈847px）该收进正文宽度\n结果：%s", out)
		}
	})

	t.Run("width 属性那种写法也认", func(t *testing.T) {
		out := CleanPastedTable(`<table width="847"><tr><td>a</td></tr></table>`)
		if !hasIgnoringSpaces(out, "width:100%") || strings.Contains(out, `width="847"`) {
			t.Errorf("属性写法的超宽也该收\n结果：%s", out)
		}
	})

	t.Run("本来就不宽的一个字不动", func(t *testing.T) {
		out := CleanPastedTable(`<table style="width:520px"><tr><td>a</td></tr></table>`)
		if !hasIgnoringSpaces(out, "width:520px") {
			t.Errorf("520px 装得下，不该改\n结果：%s", out)
		}
	})

	t.Run("本来就是百分比的一个字不动", func(t *testing.T) {
		out := CleanPastedTable(`<table style="width:80%"><tr><td>a</td></tr></table>`)
		if !hasIgnoringSpaces(out, "width:80%") {
			t.Errorf("百分比本来就装得下\n结果：%s", out)
		}
	})
}

// 认不出表格就回空串，让前端退回纯文本那条老路。空串不是错误。
func TestPastedTableSaysNothingWhenThereIsNoTable(t *testing.T) {
	for _, in := range []string{
		"", "<p>就是一段话</p>", "不是 HTML", "<div><span>没有表格</span></div>",
		strings.Repeat("x", maxPastedHTMLBytes+1), // 整份 Word 文档那种
	} {
		if out := CleanPastedTable(in); out != "" {
			t.Errorf("输入 %.30q 不该产出表格\n结果：%s", in, out)
		}
	}
}

// 净化过的东西还要能穿过**发信**白名单，否则收件人看到的又是另一回事。
//
// 两道白名单是分开的（这一道管"从外面来的"，那一道管"要发出去的"），中间
// 不合的表现是「编辑器里好看、发出去走样」——只有真发一封才看得见。
func TestPastedTableAlsoSurvivesTheOutgoingPolicy(t *testing.T) {
	in := `<table style="border-collapse:collapse;width:520px">` +
		`<tr><th colspan="2" style="font-size:18px;background-color:#eee">头</th></tr>` +
		`<tr><td width="180" style="font-size:14px">品名</td><td rowspan="2">数量</td></tr></table>`
	out := SanitizeHTML(CleanPastedTable(in))
	for _, want := range []string{
		`colspan="2"`, `rowspan="2"`, `width="180"`,
		"border-collapse", "font-size:18px", "font-size:14px", "background-color:#eee",
		"品名", "数量",
	} {
		if !hasIgnoringSpaces(out, want) {
			t.Errorf("净化过的表格发不出去，丢了 %q\n结果：%s", want, out)
		}
	}
}

// bluemonday 会把 style 规范化：`font-size:14px` 出来是 `font-size: 14px`。
// 断言里挨个写空格既啰嗦又容易漏，所以比较时把空格去掉。
func hasIgnoringSpaces(haystack, needle string) bool {
	strip := func(x string) string { return strings.ReplaceAll(x, " ", "") }
	return strings.Contains(strip(haystack), strip(needle))
}
