package app

import (
	"strings"
	"testing"
)

// Excel 复制出来的 HTML 长这样：颜色、框线、对齐写在开头一份 <style> 里，
// 每个格子只带一个 class=xl65。净化器丢 <style> 和 class 是对的（那是安全
// 边界），但丢完格子就成了白纸——2026-09-16 实测：红蓝黄字、黑框线全没了，
// 连灰框都没有，而 Foxmail 粘同一份是原样的。
//
// 所以清理之前先把每个类名对应的样式抄到格子自己身上。这个夹子是照 Excel
// 的真实输出缩的：<!-- --> 包着的样式块、td 元素规则、.xlNN 类规则、
// windowtext 和 .5pt 这种浏览器认、bluemonday 不认的写法、格子上还有一条
// 行内样式盖过类的。
const excelClipboard = `<html><head><meta charset="utf-8"><style>
<!--table
	{mso-displayed-decimal-separator:"\.";}
@page
	{margin:.75in .7in .75in .7in;
	mso-header-margin:.3in;}
td
	{padding-top:1px;
	color:black;
	font-size:11.0pt;
	font-weight:400;
	font-family:等线;
	text-align:general;
	vertical-align:middle;
	border:none;
	white-space:nowrap;}
.xl65
	{color:red;
	font-family:宋体;
	border:.5pt solid windowtext;}
.xl66
	{color:red;
	text-align:right;
	border:.5pt solid windowtext;}
.xl67
	{color:#00B0F0;
	border:.5pt solid windowtext;}
.xl68
	{font-weight:700;
	text-align:center;
	border:.5pt solid windowtext;}
-->
</style></head><body>
<table border=0 cellpadding=0 cellspacing=0 width=507 style='border-collapse:collapse;width:380pt'>
 <col width=113>
 <tr height=21 style='height:15.6pt'>
  <td height=21 class=xl68 width=113 style='height:15.6pt;width:85pt'>MEDIDAS(MM)</td>
  <td class=xl68>TONS</td>
 </tr>
 <tr height=21>
  <td class=xl65 style='border-top:none'>0.28*1220</td>
  <td class=xl66 align=right x:num>200</td>
 </tr>
 <tr>
  <td class=xl67>0.30*1220</td>
  <td class=xl66 style='color:blue'>500</td>
 </tr>
</table></body></html>`

// flat 把 bluemonday 重排过的样式（「color: red」，冒号后带空格；属性值里的
// 引号转义成 &#39;）压成好比对的样子：全小写、冒号后不留空格、引号还原。
func flat(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), ": ", ":")
	s = strings.ReplaceAll(s, "&#39;", "'")
	return strings.ReplaceAll(s, "&#34;", `"`)
}

// cellWith 找到装着这段文字的那个 <td> 的开标签（样式都在开标签上）。
func cellWith(t *testing.T, out, text string) string {
	t.Helper()
	i := strings.Index(out, text)
	if i < 0 {
		t.Fatalf("结果里没有 %q\n%s", text, out)
	}
	j := strings.LastIndex(out[:i], "<td")
	if j < 0 {
		t.Fatalf("%q 前面没有 <td\n%s", text, out)
	}
	return flat(out[j:i])
}

func TestPastedExcelTableKeepsColoursBordersAndAlignment(t *testing.T) {
	out := CleanPastedTable(excelClipboard)
	if out == "" {
		t.Fatal("Excel 的表格没认出来")
	}
	lower := flat(out)

	// 类里的颜色和框线到了格子上。
	red := cellWith(t, out, "0.28*1220")
	if !strings.Contains(red, "color:red") {
		t.Errorf("红字丢了：%s", red)
	}
	if !strings.Contains(red, "solid") {
		t.Errorf("框线丢了：%s", red)
	}
	// 格子自己那条 border-top:none 排在类的 border 之后，才盖得过它。
	if strings.Index(red, "border-top:none") < strings.Index(red, "solid") {
		t.Errorf("行内样式该排在类样式后面（后者才盖前者）：%s", red)
	}
	blue := cellWith(t, out, "0.30*1220")
	if !strings.Contains(blue, "#00b0f0") {
		t.Errorf("蓝字丢了：%s", blue)
	}
	num := cellWith(t, out, "200")
	if !strings.Contains(num, "text-align:right") {
		t.Errorf("数字靠右丢了：%s", num)
	}
	head := cellWith(t, out, "MEDIDAS")
	if !strings.Contains(head, "font-weight:700") || !strings.Contains(head, "text-align:center") {
		t.Errorf("表头加粗居中丢了：%s", head)
	}
	// 格子上行内写的 color:blue 盖过类里的 color:red——后写的赢。
	override := cellWith(t, out, "500")
	if i, j := strings.Index(override, "color:red"), strings.Index(override, "color:blue"); j < 0 || j < i {
		t.Errorf("行内 color:blue 该排在类的 color:red 后面：%s", override)
	}

	// 浏览器认、bluemonday 不认的写法要换成它认的，否则整条声明被丢。
	for _, bad := range []string{"windowtext", ":.5pt", " .5pt"} {
		if strings.Contains(lower, bad) {
			t.Errorf("%q 该换成标准写法：%s", bad, out)
		}
	}
	// 安全边界一条不松：样式块、类名、mso- 私有属性都不进来。
	for _, bad := range []string{"<style", "class=", "mso-", "x:num"} {
		if strings.Contains(lower, bad) {
			t.Errorf("%q 漏过去了：%s", bad, out)
		}
	}
}

// 样式块里的规则只认「一个类名」或「一个标签名」（可以逗号并列）。别的
// 一律不抄——:hover、后代选择器这些在邮件里没意义，抄了只会抄错。
func TestPastedTableInlinesOnlySimpleSelectors(t *testing.T) {
	in := `<html><head><style>
td:hover {color:red;}
table td {color:green;}
.a, .b {font-weight:700;}
th {text-align:center;}
</style></head><body><table><tr><th>H</th><td class="b">x</td></tr></table></body></html>`
	out := flat(CleanPastedTable(in))
	if strings.Contains(out, "color:red") || strings.Contains(out, "color:green") {
		t.Errorf("复杂选择器不该抄：%s", out)
	}
	if !strings.Contains(out, "font-weight:700") {
		t.Errorf("逗号并列的类该抄：%s", out)
	}
	if !strings.Contains(out, "text-align:center") {
		t.Errorf("标签规则该抄：%s", out)
	}
}

// 样式块里塞什么进来，最后还是要过白名单。display:none 抄到格子上也进不了结果。
func TestPastedTableInlinedStylesStillFaceThePolicy(t *testing.T) {
	in := `<html><head><style>td {display:none; color:red; background:url(https://evil.example/x)}</style></head>` +
		`<body><table><tr><td>x</td></tr></table></body></html>`
	out := flat(CleanPastedTable(in))
	for _, bad := range []string{"display", "url(", "evil.example"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q 漏过去了：%s", bad, out)
		}
	}
	if !strings.Contains(out, "color:red") {
		t.Errorf("白名单里的那条该留：%s", out)
	}
}

// WPS 和 Excel 桌面版复制出来的样式是同一个模子，但有三处上一版没照顾到：
// 填充色写的是 background 简写，不是 background-color；字体名是中文（等线）
// 或者双引号包着的（"Times New Roman"）；表头白字配深蓝底——填充色一丢就是
// 白字印在白纸上，看着像表头没了（2026-09-16 员工从 WPS 复制实测）。
const wpsClipboard = `<html><head><style>
<!--td
	{padding-top:1px;
	color:black;
	font-size:11.0pt;
	font-family:等线;
	border:none;}
.xl67
	{color:white;
	font-weight:700;
	background:#1F4E78;
	mso-pattern:black none;
	text-align:center;}
.xl65
	{color:red;
	font-family:"Times New Roman";}
-->
</style></head><body>
<table border=0 cellpadding=0 cellspacing=0 style='border-collapse:collapse;width:380pt'>
 <tr><td class=xl67>产品</td><td class=xl67>厚度</td></tr>
 <tr><td class=xl65>Hot Rolled Steel Coil</td><td>3.0</td></tr>
</table></body></html>`

func TestPastedTableKeepsFillsAndFontsFromWPS(t *testing.T) {
	out := CleanPastedTable(wpsClipboard)
	if out == "" {
		t.Fatal("WPS 的表格没认出来")
	}
	head := cellWith(t, out, "产品")
	if !strings.Contains(head, "background-color:#1f4e78") {
		t.Errorf("表头填充色丢了（background 简写该改写成 background-color）：%s", head)
	}
	if !strings.Contains(head, "color:white") {
		t.Errorf("表头白字丢了：%s", head)
	}
	if !strings.Contains(head, "font-family:等线") {
		t.Errorf("中文字体名丢了：%s", head)
	}
	red := cellWith(t, out, "Hot Rolled")
	if !strings.Contains(red, "font-family:'times new roman'") {
		t.Errorf("双引号的字体名该换成单引号留下：%s", red)
	}
	for _, bad := range []string{"mso-pattern", "background:#"} {
		if strings.Contains(flat(out), bad) {
			t.Errorf("%q 不该留下：%s", bad, out)
		}
	}
	// 发信白名单也要放行：写信时粘进去，发的时候整个正文又过一遍。
	sent := flat(SanitizeHTML(out))
	for _, want := range []string{"font-family:等线", "background-color:#1f4e78", "font-family:'times new roman'"} {
		if !strings.Contains(sent, want) {
			t.Errorf("发信白名单吃掉了 %q：%s", want, sent)
		}
	}
}

// 来源一条框线都没有（WPS/Excel 里那层灰网格线只是显示用的，复制不带）：
// 每格补一圈细灰框。邮件里一张没框的表，手机上基本对不齐列。
func TestPastedTableGetsBordersWhenTheSourceDrawsNone(t *testing.T) {
	out := CleanPastedTable(wpsClipboard)
	for _, text := range []string{"产品", "厚度", "Hot Rolled", "3.0"} {
		if c := cellWith(t, out, text); !strings.Contains(c, "border:1px solid #d0d0d0") {
			t.Errorf("%q 这格该补上框：%s", text, c)
		}
	}
	if !strings.Contains(flat(out), "border-collapse:collapse") {
		t.Errorf("表格该贴边（border-collapse），不然 1px 变 2px：%s", out)
	}
}

// 来源自己画了框的格子用它的，不盖；只画了一边的也算画了。
// border:0 不算框。
func TestPastedTableKeepsTheSourcesOwnBorders(t *testing.T) {
	in := `<html><head><style>.b{border:.5pt solid windowtext;} .l{border-left:1px dashed #333;}</style></head><body>` +
		`<table><tr><td class="b">有框</td><td class="l">左框</td><td style="border:0">零宽</td><td>没框</td></tr></table></body></html>`
	out := CleanPastedTable(in)
	if c := cellWith(t, out, "有框"); !strings.Contains(c, "border:0.5pt solid #000000") || strings.Contains(c, "#d0d0d0") {
		t.Errorf("自己有框的该原样：%s", c)
	}
	if c := cellWith(t, out, "左框"); !strings.Contains(c, "border-left:1px dashed #333") || strings.Contains(c, "#d0d0d0") {
		t.Errorf("只画一边的也算画了：%s", c)
	}
	if c := cellWith(t, out, "零宽"); !strings.Contains(c, "#d0d0d0") {
		t.Errorf("border:0 不算框，该补：%s", c)
	}
	if c := cellWith(t, out, "没框"); !strings.Contains(c, "#d0d0d0") {
		t.Errorf("没框的该补：%s", c)
	}
}

// <table border="1"> 这种老写法本身就画框（某些来源这么写），不再叠一层。
func TestPastedTableRespectsTheBorderAttribute(t *testing.T) {
	out := CleanPastedTable(`<table border="1"><tr><td>a</td></tr></table>`)
	if strings.Contains(out, "#d0d0d0") {
		t.Errorf("border=\"1\" 已经有框，不该再补：%s", out)
	}
	if !strings.Contains(out, `border="1"`) {
		t.Errorf("border=\"1\" 该留着：%s", out)
	}
}

// 字体名的审法：字母（任何文字）、数字、空格和几个标点，别的一律拒——
// 括号、斜杠、分号、尖括号正是 expression()、url() 和注入长的地方。
// bluemonday 交过来的值已经转成小写、去掉了 CSS 转义。
func TestFontFamilyValueAcceptsRealNamesAndNothingElse(t *testing.T) {
	for _, ok := range []string{"calibri", "'segoe ui', arial, sans-serif", "等线", "'微软雅黑', 宋体, sans-serif", "'times new roman'", "dengxian light", "pingfang sc, sans-serif"} {
		if !fontFamilyValue(ok) {
			t.Errorf("该放行：%q", ok)
		}
	}
	for _, bad := range []string{"expression(alert(1))", "url(x)", "a;b", "<b>", `a\b`, "a/b", "a:b", "a=b", "a&b"} {
		if fontFamilyValue(bad) {
			t.Errorf("该拒：%q", bad)
		}
	}
}
