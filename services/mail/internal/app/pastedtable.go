package app

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
)

// 从别处复制过来的表格，净化成可以直接放进邮件正文的样子。
//
// Issue #363。第一版是在浏览器里用纯文本（制表符）重建的：安全、可测，但
// 列宽、字号、合并单元格全都表达不了。这一版改成收下剪贴板里那份 text/html。
//
// **为什么净化放在服务端。** 剪贴板里的 HTML 是不可信输入，落进浏览器那个
// contenteditable 就和登录态同一个 origin——一个 <img onerror> 在那儿是会
// 执行的。所以净化不是可选项。而净化器是最不能靠"看着对"的一类代码，前端
// 测试跑在纯 node 里没有 DOMParser，写在那边测不了。这边有 x/net/html、有
// bluemonday、有一整套 Go 测试，是它该待的地方。
//
// 代价是粘贴时多一次往返。失败或者认不出表格时，前端退回纯文本那条老路。

// pastedTablePolicy 比发信白名单（mailPolicy）**窄得多**，是有意的。
//
// mailPolicy 管的是"我们自己的编辑器产出的东西"，所以它宽松到连 class 和
// mso-* 都放行（实测确实放行）。这里管的是"从 Word 和网页复制来的东西"，
// 那里面有大量只对源站点有意义的类名、私有属性和布局残渣——留着它们不会
// 变危险，只会让收件人看到一个走样的表格。
//
// 白名单而不是黑名单：想不到的东西一律丢掉，而不是一律留下。
var pastedTablePolicy = buildPastedTablePolicy()

func buildPastedTablePolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	// 表格本身，加上单元格里可能有的行内强调。段落和换行留着：一个单元格里
	// 本来就可能是两行字。
	p.AllowElements("table", "thead", "tbody", "tfoot", "tr", "td", "th",
		"caption", "colgroup", "col", "p", "br", "span", "div",
		"b", "strong", "i", "em", "u", "s", "sub", "sup", "ul", "ol", "li")

	p.AllowAttrs("colspan", "rowspan").OnElements("td", "th")
	p.AllowAttrs("width", "height", "align", "valign", "bgcolor").
		OnElements("table", "tr", "td", "th", "col", "colgroup")
	p.AllowAttrs("border", "cellpadding", "cellspacing").OnElements("table")
	p.AllowAttrs("span").OnElements("col", "colgroup")

	// **样式按属性名逐条放行**，不用 AllowStyling()。后者放行一大片，包括
	// Word 那些 mso-* 私有属性——它们对任何邮件客户端都没有意义，只是噪音。
	// 这里列的是「表格看起来还是原来那个表格」真正需要的那些。
	p.AllowStyles(
		"width", "height", "min-width", "max-width",
		"font-size", "font-family", "font-weight", "font-style",
		"text-align", "vertical-align", "text-decoration",
		"background-color", "color",
		"border", "border-top", "border-bottom", "border-left", "border-right",
		"border-collapse", "border-spacing", "border-color", "border-style", "border-width",
		"padding", "padding-top", "padding-bottom", "padding-left", "padding-right",
		"line-height", "white-space",
	).Globally()

	// 表格里可能有链接。协议限死，和发信白名单同一套。
	p.AllowAttrs("href").OnElements("a")
	p.AllowElements("a")
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireParseableURLs(true)

	// **图片一律丢掉。** 复制来的 <img> 指向源站点，或者是 Word 的
	// file:///C:/Users/... 本地路径——前者会在收件人那边变成一个坏掉的
	// 外链（而且泄露"这封信被打开了"给第三方站点），后者根本不存在。
	// 想要图就走上传那条路，那条路会把图存到我们自己这儿。

	return p
}

// CleanPastedTable 从一段剪贴板 HTML 里取出第一个表格并净化。
//
// 认不出表格就回空串，调用方据此退回纯文本那条路——**空串不是错误**，
// 粘一段普通文字进来本来就走不到这里。
func CleanPastedTable(raw string) string {
	if len(raw) > maxPastedHTMLBytes {
		// 一份 Word 文档整页复制过来能有几 MB，其中表格只占一小块。挡在
		// 解析之前：这条路是给"复制一个表格"用的，不是给"复制一整份文档"。
		return ""
	}
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return ""
	}
	// 只取表格那一棵子树。Word 会把内容裹进 <html><head><style>…</style>
	// <body><div class=WordSection1> 一层层壳里，先摘出表格再净化，那些壳
	// 连同 <style> 里的 CSS 文本就一个字都进不来。
	//
	// 顺序反过来（先净化整段、再找表格）是个经典的坑：bluemonday 丢掉
	// <style> 这个标签，但标签里那段 CSS 是文本节点，会留下来变成正文里
	// 一大段乱码。
	node := firstTable(doc)
	if node == nil {
		return ""
	}
	var sb strings.Builder
	if err := html.Render(&sb, node); err != nil {
		return ""
	}
	out := pastedTablePolicy.Sanitize(sb.String())
	if !strings.Contains(out, "<table") {
		return ""
	}
	return fitToMailWidth(out)
}

// maxPastedHTMLBytes 是愿意解析的剪贴板 HTML 上限。
//
// Word 一页正文复制过来常有几百 KB，整份文档能上 MB。2 MB 足够装下任何
// 「一个表格」，再多就不是这条路要服务的事。
const maxPastedHTMLBytes = 2 << 20

// firstTable 深度优先找第一个 <table>。
//
// 找第一个而不是最外层：Word 会用表格给整页做布局，于是真正的数据表格被
// 裹在一个一行一列的外壳表格里。深度优先先撞到外壳——所以还要往里看一眼，
// 见 unwrapLayoutTable。
func firstTable(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "table" {
		return unwrapLayoutTable(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := firstTable(c); t != nil {
			return t
		}
	}
	return nil
}

// unwrapLayoutTable 剥掉只用来做布局的外壳表格。
//
// 一个只有一行一列、而那一格里又正好只装着另一个表格的表格，不是数据，
// 是包装纸。Word 和不少网页编辑器都会生成这种。剥掉之后人看到的才是他
// 复制的那个表格，而不是一个多套了一层框的东西。
func unwrapLayoutTable(t *html.Node) *html.Node {
	rows := ownRows(t)
	if len(rows) != 1 {
		return t
	}
	cells := ownCells(rows[0])
	if len(cells) != 1 {
		return t
	}
	inner := elementChildren(cells[0])
	if len(inner) == 1 && inner[0].Data == "table" {
		return unwrapLayoutTable(inner[0])
	}
	return t
}

// ownRows 是**这一张**表格自己的行，不含嵌套表格里的行。
//
// 第一版这里用的是「把整棵子树里所有 <tr> 收上来」，于是一个"一行一列、里面
// 装着另一个表格"的外壳会数出两行（自己一行 + 里层一行），判断当场失效，
// 外壳永远剥不掉。测试当场红。
//
// 要穿过 tbody/thead/tfoot（浏览器解析时会自动补一层 tbody），但碰到
// <table> 就停——那底下是别人的行。
func ownRows(t *html.Node) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.Data {
			case "tr":
				out = append(out, c)
			case "tbody", "thead", "tfoot":
				walk(c)
			}
			// table 和别的元素都不往下走。
		}
	}
	walk(t)
	return out
}

// ownCells 同理：这一行自己的格子，不含嵌套表格里的。
func ownCells(tr *html.Node) []*html.Node {
	var out []*html.Node
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
			out = append(out, c)
		}
	}
	return out
}

func elementChildren(n *html.Node) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			out = append(out, c)
		}
	}
	return out
}

// MailBodyWidth 是邮件正文在几乎所有客户端里的宽度。超过就要横向滚动，
// 而收件人多半在手机上看。和前端 lib/pastedTable.ts 里那个是同一个数。
const MailBodyWidth = 600

// 宽度写在两个地方：属性 width="847" 和样式 width:635.25pt。两个都要认。
var (
	tableWidthAttr  = regexp.MustCompile(`(?i)\swidth="(\d+(?:\.\d+)?)"`)
	tableWidthStyle = regexp.MustCompile(`(?i)width:\s*(\d+(?:\.\d+)?)(px|pt)?`)
)

// fitToMailWidth 把一个比正文还宽的表格收进正文宽度。
//
// **不是等比缩小每一格，是把整张表改成百分比。** Excel 给的是绝对像素
// （一张按 A4 排的表轻松超过 1000px），照搬进 600px 的正文里会撑破布局，
// 在手机上更是一片横向滚动。改成 width:100% 之后，各列的相对比例由浏览器
// 按内容分配——比例可能和原表不完全一样，但"能读"比"分毫不差却读不了"要紧。
//
// 本来就不宽、或者本来就是百分比的，一个字都不动。
func fitToMailWidth(s string) string {
	if w, ok := declaredTableWidth(s); !ok || w <= MailBodyWidth {
		return s
	}
	// 只改第一个 <table> 开标签上的宽度：嵌套表格的宽度是相对外层的，
	// 一起改会把里层也拉满。
	i := strings.Index(strings.ToLower(s), "<table")
	if i < 0 {
		return s
	}
	j := strings.Index(s[i:], ">")
	if j < 0 {
		return s
	}
	tag := s[i : i+j]
	tag = tableWidthAttr.ReplaceAllString(tag, "")
	tag = tableWidthStyle.ReplaceAllString(tag, "width:100%")
	if !strings.Contains(strings.ToLower(tag), "width:") {
		tag += ` style="width:100%"`
	}
	return s[:i] + tag + s[i+j:]
}

// declaredTableWidth 读出第一个 <table> 上声明的宽度，换算成像素。
//
// pt 换 px 按 96/72：Word 用 pt，浏览器用 px，差 1.333 倍。不换算的话一张
// 635pt（≈847px）的表会被当成 635px 放过去，然后在正文里撑破。
func declaredTableWidth(s string) (float64, bool) {
	i := strings.Index(strings.ToLower(s), "<table")
	if i < 0 {
		return 0, false
	}
	j := strings.Index(s[i:], ">")
	if j < 0 {
		return 0, false
	}
	tag := s[i : i+j]
	if m := tableWidthStyle.FindStringSubmatch(tag); m != nil {
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, false
		}
		if strings.EqualFold(m[2], "pt") {
			v *= 96.0 / 72.0
		}
		return v, true
	}
	if m := tableWidthAttr.FindStringSubmatch(tag); m != nil {
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, false
		}
		return v, true
	}
	return 0, false
}
