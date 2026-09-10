package app

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// HTML 正文里**没被写成链接**的网址，补成链接。
//
// 这不是纯文本那条路。发信方写的是 HTML，但正文里那个地址是**光秃秃的文字**
// ——很多事务性邮件就是这么写的：
//
//	<p>Zoom: https://amplitude.zoom.us/j/93698437696</p>
//
// 生产上那封面试通知就是这样：is_html = 真、整封信里 <a> 标签数为 0、Zoom
// 地址在正文里但不是锚点。读的人看着像链接，点了没反应，只能手工选中复制。
//
// **按树走，不按字符串替换。** 正则扫一遍原始 HTML 会把 href="…" 里的地址、
// <style> 里的 url(…)、已经是链接的锚点文字全都再包一层，包出嵌套的 <a> 和
// 坏掉的属性。这里解析成树、只动文本节点，而且跳过那些「文本不是给人读的」
// 的容器。
//
// 跳过 <a> 的子孙是最要紧的一条：锚点里的文字本来就是链接，再包一层就是
// <a> 套 <a>，浏览器会把它拆开，结果比不动还糟。

// bareURL 是文本里看起来像网址的一段。
//
// 只认 http/https。裸写的 www. 不认——纯文本那条路认它是因为那儿除了文字
// 没有别的线索，而 HTML 里 "www." 出现在正文中间的机会多得多（"访问 www
// 站点"），认错了就是给一段普通文字加上链接。
var bareURL = regexp.MustCompile(`https?://[^\s<>"'）】]+`)

// 网址后面紧跟的标点不算网址的一部分。同前端那份的理由：带上标点就是 404。
var bareTrailing = regexp.MustCompile(`[.,;:!?。，；：！？、"'）】》]+$`)

// 这些容器里的文本不是给人读的，别动。
func skipsLinkify(n *html.Node) bool {
	switch n.DataAtom {
	case atom.A, atom.Style, atom.Script, atom.Textarea, atom.Title, atom.Head:
		return true
	}
	return false
}

// LinkifyBareURLs 把 HTML 正文里裸写的网址补成可点的链接。
//
// 输入应当是已经过 SanitizeForReading 的正文：这里只加锚点，不做任何净化。
func LinkifyBareURLs(s string) string {
	if !strings.Contains(s, "http") {
		return s
	}
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		// 解析不了就原样退回。补链接是锦上添花，不值得为它丢掉一封信的正文。
		return s
	}
	body := findBody(doc)
	if body == nil {
		return s
	}
	if !linkifyNode(body) {
		// 一个都没补上时原样退回，避免白白经历一次「解析再序列化」——那一趟
		// 会把原文规范化（补全标签、改引号），对没有变化的正文是无谓的扰动。
		return s
	}

	var sb strings.Builder
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&sb, c); err != nil {
			return s
		}
	}
	return sb.String()
}

// linkifyNode 递归处理，返回有没有真的改过东西。
func linkifyNode(n *html.Node) bool {
	changed := false
	// 先把孩子收集出来再遍历：下面会往链表里插节点，边插边走会漏掉后面的。
	var kids []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		kids = append(kids, c)
	}
	for _, c := range kids {
		switch {
		case c.Type == html.ElementNode && skipsLinkify(c):
			// 整棵跳过
		case c.Type == html.ElementNode:
			if linkifyNode(c) {
				changed = true
			}
		case c.Type == html.TextNode:
			if replaceTextNode(n, c) {
				changed = true
			}
		}
	}
	return changed
}

// replaceTextNode 把一个文本节点拆成「文字 + 锚点 + 文字…」插回去。
func replaceTextNode(parent, node *html.Node) bool {
	text := node.Data
	spans := bareURL.FindAllStringIndex(text, -1)
	if len(spans) == 0 {
		return false
	}

	var made []*html.Node
	last := 0
	for _, span := range spans {
		raw := text[span[0]:span[1]]
		url := bareTrailing.ReplaceAllString(raw, "")
		// 括号配对：地址本身可以带括号（维基百科那种），只有多出来的右括号
		// 才算句子的。
		for strings.HasSuffix(url, ")") &&
			strings.Count(url, "(") < strings.Count(url, ")") {
			url = strings.TrimSuffix(url, ")")
		}
		if url == "" {
			continue
		}
		if before := text[last:span[0]]; before != "" {
			made = append(made, &html.Node{Type: html.TextNode, Data: before})
		}
		a := &html.Node{
			Type: html.ElementNode, DataAtom: atom.A, Data: "a",
			Attr: []html.Attribute{
				{Key: "href", Val: url},
				{Key: "target", Val: "_blank"},
				// 新标签页不该拿到我们这一页的引用，也不该把我们的地址带给
				// 对方——信是别人写的，链接指向哪儿不由我们决定。
				{Key: "rel", Val: "noopener noreferrer"},
			},
		}
		a.AppendChild(&html.Node{Type: html.TextNode, Data: url})
		made = append(made, a)
		last = span[0] + len(url)
	}
	if len(made) == 0 {
		return false
	}
	if tail := text[last:]; tail != "" {
		made = append(made, &html.Node{Type: html.TextNode, Data: tail})
	}

	for _, m := range made {
		parent.InsertBefore(m, node)
	}
	parent.RemoveChild(node)
	return true
}
