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

// flat 把 bluemonday 重排过的样式（「color: red」，冒号后带空格）压成好比对的
// 样子：全小写、冒号后不留空格。
func flat(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), ": ", ":")
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
