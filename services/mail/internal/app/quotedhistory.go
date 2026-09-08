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

const (
	// Below this, the fold is not worth its own button. A four-line quote at
	// the bottom of a two-line reply is context, not clutter.
	minQuotedChars = 400
)

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
	if len([]rune(textOf(quotedFrag))) < minQuotedChars || !freshEnoughToStandAlone(freshFrag) {
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
	// The boundary is looked for among the body's own children rather than
	// anywhere in the tree. A <blockquote> nested three divs deep inside a
	// marketing layout is part of that layout; splitting there would cut the
	// layout in half. This costs the occasional missed fold on a deeply
	// wrapped reply, which is the cheaper mistake.
	start := quoteBoundary(body)
	if start == nil {
		return "", "", false
	}

	var freshSB, quotedSB strings.Builder
	inQuote := false
	for c := body.FirstChild; c != nil; c = c.NextSibling {
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
	return freshSB.String(), quotedSB.String(), true
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
