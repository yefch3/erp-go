package app

import (
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// Formats a body can be stored in.
const (
	FormatText = "TEXT"
	FormatHTML = "HTML"
)

// mailPolicy is the whitelist of what may survive into an outgoing mail.
//
// It is deliberately narrower than a general-purpose HTML policy, for two
// separate reasons that happen to point the same way:
//
//   - Security. The composed body is stored, shown back in the UI, and sent
//     to third parties. Script, iframes, event handlers and javascript: URLs
//     have no business in any of those.
//   - Rendering. Outlook renders with Word's engine and Gmail strips <style>
//     blocks, so anything outside this set either does not work or works in
//     one client and not the next. Allowing a tag we cannot render is worse
//     than refusing it: it looks fine in the composer and arrives broken.
//
// The client-side editor is built to produce exactly this shape. It is not
// trusted to: this runs on every write.
var mailPolicy = buildMailPolicy()

func buildMailPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	// Inline emphasis and structure that every mail client renders.
	p.AllowElements("p", "br", "div", "span", "b", "strong", "i", "em",
		"u", "s", "strike", "sub", "sup", "blockquote", "hr",
		"h1", "h2", "h3", "h4", "ul", "ol", "li", "pre", "code")

	// Tables are the only layout mechanism that works everywhere. Flexbox and
	// grid are not options in mail, however modern the composer looks.
	p.AllowElements("table", "thead", "tbody", "tr", "td", "th")
	p.AllowAttrs("width", "height", "align", "valign", "bgcolor",
		"cellpadding", "cellspacing", "border").OnElements(
		"table", "tr", "td", "th")
	p.AllowAttrs("colspan", "rowspan").OnElements("td", "th")

	// Inline styles only — a <style> block would be stripped by Gmail anyway.
	// bluemonday validates the CSS property list itself.
	p.AllowStyling()
	p.AllowAttrs("style").Globally()

	// Links: http/https/mailto only. Requiring a scheme is what keeps
	// javascript: out, and it stays out even if the editor is bypassed.
	p.AllowAttrs("href").OnElements("a")
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireParseableURLs(true)
	p.RequireNoFollowOnLinks(false)
	// Links in mail open outside the client anyway; target is advisory.
	p.AllowAttrs("target").Matching(regexp.MustCompile(`^_blank$`)).OnElements("a")

	// Images. alt is not decoration: mail clients block images by default,
	// so alt text is what most recipients actually see the first time.
	p.AllowImages()
	p.AllowAttrs("src", "alt", "width", "height", "border").OnElements("img")

	return p
}

// SanitizeHTML strips anything outside the mail whitelist.
func SanitizeHTML(s string) string { return mailPolicy.Sanitize(s) }

// normalizeFormat coerces an arbitrary caller-supplied value.
func normalizeFormat(f string) string {
	if strings.ToUpper(strings.TrimSpace(f)) == FormatHTML {
		return FormatHTML
	}
	return FormatText
}

// ---------------------------------------------------------------- text part

var (
	// </li> is absent on purpose: the opening <li> already starts the line,
	// so closing it too would double-space every list.
	blockEnd   = regexp.MustCompile(`(?i)</(p|div|tr|h[1-6]|blockquote|pre)\s*>`)
	brTag      = regexp.MustCompile(`(?i)<br\s*/?>`)
	listItem   = regexp.MustCompile(`(?i)<li[^>]*>`)
	anyTag     = regexp.MustCompile(`<[^>]*>`)
	manyBlanks = regexp.MustCompile(`\n{3,}`)
	trailingWS = regexp.MustCompile(`[ \t]+\n`)
	// Elements whose content is invisible in the rendered mail and so must
	// not appear in the text part either. One pattern each: Go's regexp is
	// RE2, which has no backreferences, so a single `</\1>` form is out.
	invisible = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`),
		regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`),
		regexp.MustCompile(`(?is)<head[^>]*>.*?</head>`),
	}
)

// HTMLToText derives the plain-text alternative from the HTML body.
//
// Every HTML mail must carry one. Sending HTML alone is a measurable spam
// signal, and a recipient reading in text mode would otherwise get a wall of
// markup. Deriving it beats asking the sender to write the mail twice, which
// in practice means the second version rots.
//
// This is deliberately crude — it is a fallback, not a renderer. Block ends
// become newlines, tags go, entities come back.
func HTMLToText(h string) string {
	s := h
	// Anything invisible in the rendered mail must not appear in the text.
	for _, re := range invisible {
		s = re.ReplaceAllString(s, "")
	}
	s = brTag.ReplaceAllString(s, "\n")
	s = listItem.ReplaceAllString(s, "\n• ")
	s = blockEnd.ReplaceAllString(s, "\n")
	s = anyTag.ReplaceAllString(s, "")
	s = unescapeEntities(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = trailingWS.ReplaceAllString(s, "\n")
	s = manyBlanks.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// unescapeEntities covers the handful the sanitiser and the editor emit.
// html.UnescapeString would also do numeric entities, but pulling in the
// package for five replacements is not worth the import.
var entities = strings.NewReplacer(
	"&nbsp;", " ",
	"&amp;", "&",
	"&lt;", "<",
	"&gt;", ">",
	"&quot;", `"`,
	"&#39;", "'",
	"&#160;", " ",
)

func unescapeEntities(s string) string { return entities.Replace(s) }

// ---------------------------------------------------------------- escaping

// htmlEscaper protects a substituted value from breaking the surrounding
// markup. A customer legitimately called "Smith & Sons <Trading>" would
// otherwise mangle the layout, and a hostile one could inject.
var htmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&#39;",
)

func escapeForHTML(s string) string { return htmlEscaper.Replace(s) }
