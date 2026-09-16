package app

import (
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// 把样式块里的规则抄到格子自己身上，然后才净化。
//
// Excel 和 Word 复制出来的 HTML 把颜色、框线、对齐写在开头一份 <style> 里，
// 格子上只有一个 class=xl65。净化器丢 <style> 和 class 是安全边界，不能松；
// 但丢完格子就成了白纸——2026-09-16 实测：红蓝黄字、黑框线全没了，连灰框
// 都没有，而 Foxmail 粘同一份是原样的（它把样式块整个留着）。
//
// 所以在丢之前，把每个类名对应的样式抄成行内样式。之后照旧过白名单：抄
// 进来的东西一样一条条被审，display:none、url() 这些照样进不来
// （TestPastedTableInlinedStylesStillFaceThePolicy）。
//
// 只认最简单的规则：一个类名（.xl65）或一个标签名（td），可以逗号并列。
// :hover、后代选择器、@page 这些在邮件里没意义，抄了只会抄错。
//
// 顺序按 CSS 的层叠：标签规则、类规则、格子自己的行内样式，后写的赢。Excel
// 正是靠这个让格子上一条 border-top:none 盖过类里的 border。

var (
	cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	// 一层花括号：选择器 { 声明 }。@ 规则在这之前已经整块删掉，所以不会有嵌套。
	cssRule = regexp.MustCompile(`(?s)([^{}]+)\{([^{}]*)\}`)
	// 一个类名，或一个标签名。
	simpleSelector = regexp.MustCompile(`^(\.[A-Za-z_][\w-]*|[A-Za-z][A-Za-z0-9]*)$`)
	// 两种浏览器认、bluemonday 不认的写法，不换的话整条声明被丢：
	// windowtext 是 Windows 的系统色，等于黑；.5pt 省掉了前导 0。
	cssWindowText = regexp.MustCompile(`(?i)\bwindowtext\b`)
	cssLeadingDot = regexp.MustCompile(`(^|[\s:,(])\.(\d)`)
	// Excel 和 WPS 把单元格填充色写成 background 简写（background:#1F4E78），
	// 白名单只认 background-color——不改写的话填充色整条被丢，表头的白字就
	// 印在了白纸上（2026-09-16 员工从 WPS 复制实测）。只改「值就是一个颜色」
	// 的：简写里还可能带图片，那种本来就不该进邮件。
	cssBackgroundColourOnly = regexp.MustCompile(`(?i)^background\s*:\s*(#[0-9a-f]{3,8}|rgba?\([^)]*\)|[a-z]+)$`)
	// 字体名：字母（任何文字，等线、宋体也是字母）、数字、空格和几个标点。
	// 括号、斜杠、分号、尖括号一律不许——expression()、url()、注入都长在那些
	// 字符上。bluemonday 交过来的值已经转成小写、去掉了 CSS 转义。
	cssFontFamily = regexp.MustCompile(`^[\p{L}\p{N} _\-.,'"]*$`)
	// 一条框线声明。值里有线型就算画了框，除非宽度是 0。
	cssBorderDecl = regexp.MustCompile(`(?i)(^|;)\s*border(-top|-right|-bottom|-left)?\s*:\s*([^;]*)`)
)

// fontFamilyValue 是 font-family 的审法，两个白名单（粘贴清理、发信）共用。
//
// bluemonday 自带的那条正则只认 ASCII 字母和单引号，等线、宋体、"Times New
// Roman"（双引号）全过不去，整条被丢，粘进来的表格字体就变了。
func fontFamilyValue(v string) bool {
	return cssFontFamily.MatchString(v)
}

// pastedCellBorder 是来源没画框时补的那圈框。和从前纯文本重建那条路用的
// 同一个灰，收件人在手机上才对得齐列。
const pastedCellBorder = "border:1px solid #d0d0d0"

// drawsBorder 说这段样式有没有画出可见的框线：有线型（solid/dashed/dotted/
// double），而且宽度不是 0。border:none、border:0 都不算。
func drawsBorder(style string) bool {
	for _, m := range cssBorderDecl.FindAllStringSubmatch(style, -1) {
		val := strings.ToLower(m[3])
		lined := false
		for _, kind := range []string{"solid", "dashed", "dotted", "double"} {
			if strings.Contains(val, kind) {
				lined = true
			}
		}
		if !lined {
			continue
		}
		zero := false
		for _, tok := range strings.Fields(val) {
			switch tok {
			case "0", "0px", "0pt", "0em", "0in", "0cm", "0mm":
				zero = true
			}
		}
		if !zero {
			return true
		}
	}
	return false
}

// ensureCellBorders 给来源没画框的格子补一圈细灰框。
//
// Excel 和 WPS 里那层灰网格线只是显示用的，复制不带；只有用「边框」功能画
// 上去的才会跟着来。邮件里一张没框的表，客户在手机上基本对不齐列，所以
// 没框的补上；来源自己画了的（哪怕只画一边）用它的。<table border="1">
// 这种老写法本身就画框，不再叠一层。
func ensureCellBorders(table *html.Node) {
	if b, err := strconv.Atoi(strings.TrimSpace(nodeAttr(table, "border"))); err == nil && b > 0 {
		return
	}
	// 表格本身要贴边，不然相邻两格的 1px 变 2px。
	if ts := nodeAttr(table, "style"); !strings.Contains(strings.ToLower(ts), "border-collapse") {
		setNodeAttr(table, "style", joinDecls(ts, "border-collapse:collapse"))
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "td" || n.Data == "th") {
			if style := nodeAttr(n, "style"); !drawsBorder(style) {
				setNodeAttr(n, "style", lastDeclWins(joinDecls(style, pastedCellBorder)))
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(table)
}

func joinDecls(style, extra string) string {
	style = normaliseDecls(style)
	if style == "" {
		return extra
	}
	return style + ";" + extra
}

// styleRules 是 选择器 → 声明串（已规范化，分号分隔，末尾不带分号）。
// 标签名一律小写，类名保持原样（CSS 里类名是区分大小写的）。
type styleRules map[string]string

// stylesheetRules 收齐文档里每个 <style> 块的简单规则。
func stylesheetRules(doc *html.Node) styleRules {
	rules := styleRules{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "style" {
			var sb strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					sb.WriteString(c.Data)
				}
			}
			parseRulesInto(rules, sb.String())
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return rules
}

func parseRulesInto(rules styleRules, css string) {
	// Excel 把整段 CSS 包在 <!-- --> 里（给上古浏览器看的），解析前先剥掉。
	css = strings.ReplaceAll(css, "<!--", "")
	css = strings.ReplaceAll(css, "-->", "")
	css = cssComment.ReplaceAllString(css, "")
	css = dropAtRules(css)
	for _, m := range cssRule.FindAllStringSubmatch(css, -1) {
		decls := normaliseDecls(m[2])
		if decls == "" {
			continue
		}
		for _, sel := range strings.Split(m[1], ",") {
			sel = strings.TrimSpace(sel)
			if !simpleSelector.MatchString(sel) {
				continue
			}
			if !strings.HasPrefix(sel, ".") {
				sel = strings.ToLower(sel)
			}
			if prev, ok := rules[sel]; ok {
				rules[sel] = prev + ";" + decls
			} else {
				rules[sel] = decls
			}
		}
	}
}

// dropAtRules 把 @page、@font-face、@media { … { … } } 这类规则连同整块删掉。
//
// 单独处理是因为 @media 里面还有一层花括号，交给 cssRule 那个「一层」的
// 正则会把外层选择器和里层规则错配。@ 规则在邮件正文里本来也没有意义。
func dropAtRules(css string) string {
	var sb strings.Builder
	for {
		i := strings.Index(css, "@")
		if i < 0 {
			sb.WriteString(css)
			return sb.String()
		}
		sb.WriteString(css[:i])
		rest := css[i:]
		open := strings.Index(rest, "{")
		if open < 0 {
			return sb.String()
		}
		depth, end := 0, -1
		for j := open; j < len(rest); j++ {
			switch rest[j] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = j
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			return sb.String()
		}
		css = rest[end+1:]
	}
}

// normaliseDecls 把一段声明压成一行：去掉换行和多余空白、空声明，换掉
// bluemonday 不认的写法。
func normaliseDecls(body string) string {
	body = cssWindowText.ReplaceAllString(body, "#000000")
	body = cssLeadingDot.ReplaceAllString(body, "${1}0.${2}")
	var out []string
	for _, d := range strings.Split(body, ";") {
		d = strings.Join(strings.Fields(d), " ")
		if d == "" {
			continue
		}
		if m := cssBackgroundColourOnly.FindStringSubmatch(d); m != nil {
			d = "background-color:" + m[1]
		}
		// 双引号的字体名换成单引号：这段最后要塞进一个双引号包着的 style
		// 属性里，少一层转义少一处出错。
		if strings.HasPrefix(strings.ToLower(d), "font-family") {
			d = strings.ReplaceAll(d, `"`, `'`)
		}
		out = append(out, d)
	}
	return strings.Join(out, ";")
}

// inlineRules 把规则抄到这棵子树里每个元素的 style 上。元素自己原有的行内
// 样式排在最后，所以盖得过抄来的。没有任何规则命中的元素，行内样式也过一遍
// 规范化——格子上直接写的 .5pt 一样要换。
func inlineRules(n *html.Node, rules styleRules) {
	if n.Type == html.ElementNode {
		var decls []string
		if d, ok := rules[strings.ToLower(n.Data)]; ok {
			decls = append(decls, d)
		}
		for _, cls := range strings.Fields(nodeAttr(n, "class")) {
			if d, ok := rules["."+cls]; ok {
				decls = append(decls, d)
			}
		}
		if own := normaliseDecls(nodeAttr(n, "style")); own != "" {
			decls = append(decls, own)
		}
		if len(decls) > 0 {
			setNodeAttr(n, "style", lastDeclWins(strings.Join(decls, ";")))
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		inlineRules(c, rules)
	}
}

// lastDeclWins 同一个属性写了几次只留最后那次，位置也按最后那次算。
//
// 层叠的结果本来就是这样（td 规则的 border:none 被类的 border:0.5pt 盖掉），
// 提前算好只是让发出去的正文短一些：Excel 的表一格十几条声明，其中一半是
// 被盖掉的。
func lastDeclWins(style string) string {
	type decl struct{ prop, full string }
	var order []decl
	last := map[string]int{}
	for _, d := range strings.Split(style, ";") {
		prop, _, ok := strings.Cut(d, ":")
		if !ok {
			continue
		}
		prop = strings.ToLower(strings.TrimSpace(prop))
		if i, seen := last[prop]; seen {
			order = append(order[:i], order[i+1:]...)
			for k, v := range last {
				if v > i {
					last[k] = v - 1
				}
			}
		}
		last[prop] = len(order)
		order = append(order, decl{prop, d})
	}
	out := make([]string, len(order))
	for i, d := range order {
		out[i] = d.full
	}
	return strings.Join(out, ";")
}

func nodeAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func setNodeAttr(n *html.Node, key, val string) {
	for i, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}
