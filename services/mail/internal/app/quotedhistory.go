package app

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Folding the quoted history out of a received mail.
//
// Every reply carries the whole conversation inside it. Sixteen turns in, the
// last message is fifteen copies of the first fourteen — measured on a real
// thread here, one message came to 10747 pixels, thirteen screens, of which
// the part somebody had actually written was the top two inches. Gmail's "•••"
// exists for exactly this.
//
// Two decisions shape this file.
//
// **It parses rather than cuts.** The obvious implementation finds the index
// where the quote starts and slices the string. That produces unbalanced
// markup in both halves — an open <div> above, a stray </div> below — and the
// halves are then rendered as two separate documents, where the browser
// repairs each one differently and the layout of a marketing mail collapses.
// So the body is parsed, split at a node boundary, and re-serialised. Both
// halves are then well-formed by construction.
//
// **It would rather fold nothing than fold the wrong thing.** A reader who has
// to click to see the sentence somebody wrote them is worse off than one who
// scrolls. So the checks below are conservative on purpose: an unrecognised
// shape is left whole, and even a recognised one is left whole when the part
// above it is too thin to stand on its own.

// 折叠曾经有一条下限：引用的可读文字不到 400 个字符就不折，理由是「短引用是
// 上下文，不是杂物」。这条已经退役，因为它犯的是这个文件下面刚记过的那个错。
//
// 400 是按字符数算的。这个产品的用户写中文——三十个汉字是一整段话，四百个
// 汉字是一篇文章。同一段引用，英文写出来轻松过线，中文写出来永远不到，于是
// 中文用户看到的引用从来不折。freshEnoughToStandAlone 上面记的是同一件事：
// 拿字符数当分量的尺子，在一个字顶一个词的语言里量不准。
//
// 换掉它的不是一个更好的数，是不要这个数。Gmail 的「•••」也不看长短：认出了
// 引用就折。这样还多一个好处——什么时候有那个按钮变得可预测了，而原来是
// 「有时候有，有时候没有」，用户猜不出规律。
//
// 剩下的唯一一道闸是 freshEnoughToStandAlone：上面什么都没有的纯转发不折，
// 否则会折出一个只有按钮、没有内容的页面。

// freshEnoughToStandAlone reports whether there is anything above the fold.
//
// The only thing this guards against is a mail that is *nothing but* a
// forward: there the boundary is the first thing in the body, so folding would
// leave a page with a button and no content, which reads as broken rather than
// as tidy.
//
// It was a character count at first — at least twenty-four — and that was
// wrong in a way worth recording, because it is the second time this codebase
// has made it. "Confirmed, please ship Monday." is thirty characters and
// passed. "好的，周一发货。" says the same thing in eight and did not, so a
// Chinese reply above a long quote never folded. A flat character minimum is
// not a measure of substance in a language that carries a word per character,
// and the users this product is for write in that language.
//
// So the bar is only "is there anything here at all". A reply of "ok" above
// fifteen quoted turns is still a reply, and still wants the fold.
func freshEnoughToStandAlone(fresh string) bool {
	return strings.TrimSpace(textOf(fresh)) != ""
}

// quoteOpeners are the phrases a mail client writes immediately above the
// history it is about to quote. Matched against the text of one block, lower
// case, so a heading in the middle of a sentence cannot trigger a fold.
//
// The list is short and deliberately regional: this mailbox receives from
// Gmail, Outlook, Apple Mail, 263 and QQ, and every one of them writes one of
// these.
var quoteOpeners = []*regexp.Regexp{
	// "On Mon, 3 Mar 2026 at 10:30, Ana Costa <ana@buyer.com> wrote:"
	regexp.MustCompile(`(?i)^on\s.{0,120}\swrote:`),
	// Apple Mail's shorter form, and the German/Spanish equivalents seen here.
	regexp.MustCompile(`(?i)^on\s.{0,80}\s(wrote|escribió):`),
	regexp.MustCompile(`(?i)^-{2,}\s*original message\s*-{2,}`),
	regexp.MustCompile(`(?i)^-{2,}\s*forwarded message\s*-{2,}`),
	regexp.MustCompile(`(?i)^begin forwarded message:`),
	// Outlook, which writes a header block rather than a sentence.
	regexp.MustCompile(`(?i)^from:\s.{0,200}\bsent:`),
	regexp.MustCompile(`(?i)^from:\s.{0,200}\bto:`),
	// 中文客户端。QQ 邮箱、Foxmail、263 各写一种，分隔线的横杠数量不固定。
	regexp.MustCompile(`^-{2,}\s*(原始邮件|原邮件|以下为引用内容)\s*-{2,}`),
	regexp.MustCompile(`^在\s?\d{4}.{0,80}(写道|寫道)[:：]`),
	// "CEO <ceo@corp.example> 写道：" —— ERP 自己的写信框加的那一行，Foxmail
	// 和几家网页版也是这个写法。上面那几条都要求以「在 + 年份」开头，这一条
	// 不要求：这个格式里没有日期，只有一个人和一个地址。带尖括号的地址是
	// 它和普通句子的分界，不至于把正文里一句「他写道：」当成引用的开头。
	regexp.MustCompile(`(?i)^\S.{0,120}<[^<>@\s]+@[^<>@\s]+>\s*(写道|寫道|wrote)\s*[:：]\s*$`),
	regexp.MustCompile(`^发件人[:：].{0,200}(发送时间|收件人)[:：]`),
	regexp.MustCompile(`^寄件者[:：]`),
}

// quoteClasses are the containers clients wrap history in. Class and id both,
// because Outlook uses an id and Gmail a class.
var quoteClasses = []string{
	"gmail_quote",                         // Gmail
	"gmail_extra",                         // older Gmail
	"moz-cite-prefix",                     // Thunderbird
	"yahoo_quoted",                        // Yahoo
	"ms-outlook-mobile-reference-message", // Outlook mobile
	"quoted",                              // several Chinese clients
	"divrplyfwdmsg",                       // Outlook desktop (an id)
	"appendonsend",                        // Outlook's marker for "everything below is history"
}

// SplitQuotedHistory divides a received mail into what was written now and
// what is being quoted back.
//
// Returns the whole body as fresh and an empty quote when there is nothing
// worth folding — which is the answer for most mail, and the answer whenever
// the shape is not recognised.
//
// The stylesheet a sanitised body carries is copied onto both halves: they are
// rendered as two documents, and a quote without the sender's CSS would open
// into something that looks nothing like the mail it came from.
func SplitQuotedHistory(body string) (fresh, quoted string) {
	style, rest := splitLeadingStyle(body)
	freshFrag, quotedFrag, ok := splitFragment(rest)
	if !ok {
		return body, ""
	}
	if !freshEnoughToStandAlone(freshFrag) {
		return body, ""
	}
	return style + freshFrag, style + quotedFrag
}

// splitLeadingStyle lifts the <style> block SanitizeForReading puts in front
// of the body, so it can be given to both halves.
func splitLeadingStyle(s string) (style, rest string) {
	trimmed := strings.TrimLeft(s, " \t\r\n")
	if !strings.HasPrefix(strings.ToLower(trimmed), "<style") {
		return "", s
	}
	end := strings.Index(strings.ToLower(trimmed), "</style>")
	if end < 0 {
		return "", s
	}
	end += len("</style>")
	return trimmed[:end], trimmed[end:]
}

// splitFragment parses the body, finds where the history begins, and returns
// the two halves serialised.
func splitFragment(fragment string) (fresh, quoted string, ok bool) {
	doc, err := html.Parse(strings.NewReader(fragment))
	if err != nil {
		return "", "", false
	}
	body := findBody(doc)
	if body == nil {
		return "", "", false
	}
	// The boundary is looked for among one container's own children rather
	// than anywhere in the tree. A <blockquote> nested three divs deep inside a
	// marketing layout is part of that layout; splitting there would cut the
	// layout in half.
	//
	// 从前那个容器只能是 <body>。这漏掉的不是「偶尔一封包得很深的回复」，而是
	// **Gmail 自己的一种常规写法**：回复有时整个套在一层 <div dir="ltr"> 里——
	// 新写的几行、<br>、引用块三样是它的子节点，而不是 body 的。于是 body 只有
	// 一个孩子，那个孩子既不是 blockquote 也没有引用类名，折叠放弃。会话里
	// 同一个人先后两封 Gmail 回复，一封折了、一封把整条历史又摊了一遍。
	//
	// 所以只有一个纯包装层时往里看一层（见 boundaryWithin）。营销邮件仍然不会
	// 被切：它那层包装底下是一张表，表不是包装，到那儿就停。
	container, start := boundaryWithin(body)
	if start == nil {
		return "", "", false
	}

	var freshSB, quotedSB strings.Builder
	inQuote := false
	for c := container.FirstChild; c != nil; c = c.NextSibling {
		if c == start {
			inQuote = true
		}
		target := &freshSB
		if inQuote {
			target = &quotedSB
		}
		if err := html.Render(target, c); err != nil {
			return "", "", false
		}
	}
	// 剥掉的那几层包装原样套回两半。dir="ltr" 这种属性丢了看不出来，但包装上
	// 若带着 style（字体、颜色），丢了两半就不像同一封信了——和上面把 <style>
	// 复制到两半是同一个道理。
	open, close := wrappersBetween(body, container)
	return open + freshSB.String() + close, open + quotedSB.String() + close, true
}

// maxWrapperDepth 是往里剥几层包装。Gmail 是一层；没见过真信超过两层。
// 有上限是为了不在一封故意套了一百层 div 的信上打转。
const maxWrapperDepth = 3

// boundaryWithin 找出引用从哪个容器的哪个孩子开始。
//
// 先看当前这一层；这一层找不到、而它恰好只有一个纯包装的孩子，就进去再看。
// **顺序是要紧的**：一封纯转发（上面什么都没写）的引用块本身可能就是那个
// 唯一的孩子，先在外层认出它，freshEnoughToStandAlone 才拦得住；先钻进去的话
// 会在引用块**里面**找到更深一层的 blockquote，把「某某写道：」那一行当成新写
// 的正文折出来。
func boundaryWithin(node *html.Node) (container, start *html.Node) {
	for depth := 0; depth <= maxWrapperDepth; depth++ {
		if start := quoteBoundary(node); start != nil {
			return node, start
		}
		inner := soleWrapperChild(node)
		if inner == nil {
			return nil, nil
		}
		node = inner
	}
	return nil, nil
}

// soleWrapperChild 是这个节点唯一的元素孩子，且那孩子是个纯包装（div 之类）；
// 其余情况回 nil。「唯一」把空白文本和注释不算在内——Gmail 在标签之间留换行。
//
// 只认这几个标签：它们不带布局含义，包在外面只是客户端的习惯。表格、列表、
// 段落都不是——一张表底下的东西是表的一部分，不该被切开。
func soleWrapperChild(n *html.Node) *html.Node {
	var only *html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			if strings.TrimSpace(c.Data) != "" {
				return nil
			}
		case html.CommentNode:
		case html.ElementNode:
			if only != nil {
				return nil
			}
			only = c
		default:
			return nil
		}
	}
	if only == nil {
		return nil
	}
	switch only.DataAtom {
	case atom.Div, atom.Span, atom.Section, atom.Article, atom.Font, atom.Center:
		return only
	}
	return nil
}

// wrappersBetween 把 outer（不含）到 inner（含）之间的每一层包装渲染成一对
// 开合标签，外层在前。两半各自套上这一串，就和剥之前长得一样。
func wrappersBetween(outer, inner *html.Node) (open, close string) {
	for n := inner; n != nil && n != outer; n = n.Parent {
		o, c := tagPair(n)
		open = o + open
		close = close + c
	}
	return open, close
}

// tagPair 渲染一个元素的开标签和闭标签，不带孩子。借 html.Render 的手，好让
// 属性的转义和它渲染正文时完全一致。
func tagPair(n *html.Node) (open, close string) {
	shell := &html.Node{Type: html.ElementNode, DataAtom: n.DataAtom, Data: n.Data, Attr: n.Attr}
	var sb strings.Builder
	if err := html.Render(&sb, shell); err != nil {
		return "", ""
	}
	s := sb.String()
	// 空元素渲染出来是 <div a="b"></div>，最后一个 < 之前是开标签。
	if i := strings.LastIndex(s, "</"); i > 0 {
		return s[:i], s[i:]
	}
	return s, ""
}

// quoteBoundary is the first child of the body from which everything is
// history, or nil when there is no such child.
func quoteBoundary(body *html.Node) *html.Node {
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if c.DataAtom == atom.Blockquote || hasQuoteMarker(c) {
			return c
		}
		// A phrase like "On ... wrote:" introduces the quote, so the fold has
		// to start at the phrase and not after it — otherwise the line
		// "On Monday Ana wrote:" is left dangling above a fold with nothing
		// under it.
		if opensQuote(textOf(renderNode(c))) {
			return c
		}
		// Outlook and several Chinese clients draw a rule and put the history
		// after it. The rule alone is not enough — plenty of signatures are
		// preceded by one — so it only counts when the block after it reads
		// like a quoted header.
		if c.DataAtom == atom.Hr {
			if next := nextElement(c); next != nil && opensQuote(textOf(renderNode(next))) {
				return c
			}
		}
	}
	return nil
}

func hasQuoteMarker(n *html.Node) bool {
	for _, a := range n.Attr {
		if a.Key != "class" && a.Key != "id" {
			continue
		}
		v := strings.ToLower(a.Val)
		for _, want := range quoteClasses {
			// Substring rather than exact: Gmail writes
			// class="gmail_quote gmail_quote_container".
			if strings.Contains(v, want) {
				return true
			}
		}
	}
	return false
}

// opensQuote tests the first line of a block, not the whole thing. A reply
// that happens to contain the words "original message" in its third paragraph
// is not announcing a quote.
func opensQuote(text string) bool {
	line := strings.TrimSpace(firstMeaningfulLine(text))
	if line == "" {
		return false
	}
	for _, re := range quoteOpeners {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func firstMeaningfulLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			// Outlook's header block runs "From: … Sent: … To: …" across
			// several lines; the patterns for it span the whole block, so a
			// generous slice is handed over rather than one line.
			return collapseSpaces(text)
		}
	}
	return ""
}

var manySpaces = regexp.MustCompile(`[ \t\r\n\x{00a0}]+`)

func collapseSpaces(s string) string {
	return strings.TrimSpace(manySpaces.ReplaceAllString(s, " "))
}

func nextElement(n *html.Node) *html.Node {
	for c := n.NextSibling; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			return c
		}
	}
	return nil
}

func findBody(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == atom.Body {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if b := findBody(c); b != nil {
			return b
		}
	}
	return nil
}

func renderNode(n *html.Node) string {
	var sb strings.Builder
	if err := html.Render(&sb, n); err != nil {
		return ""
	}
	return sb.String()
}

// textOf is the visible words of a fragment, used only for the size and
// phrase checks. HTMLToText is the same conversion the text alternative uses,
// so "how long is this" is answered the same way everywhere.
func textOf(fragment string) string { return HTMLToText(fragment) }
