package app

import (
	"html"
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
func SanitizeHTML(s string) string { return mailPolicy.Sanitize(repairURLWhitespace(s)) }

// urlAttr finds the value of a src or href, in either quote style.
var urlAttr = regexp.MustCompile(`(?is)\b(src|href)\s*=\s*("([^"]*)"|'([^']*)')`)

// asciiWhitespaceInURL is what browsers refuse to leave in a URL.
var asciiWhitespaceInURL = regexp.MustCompile(`[ \t\r\n\f]`)

// repairURLWhitespace percent-encodes spaces and drops tabs and newlines
// inside src and href values, the way a browser does before fetching.
//
// The policy sets RequireParseableURLs, which is what keeps javascript: out
// and must stay on. Its cost is that net/url rejects a raw space, and
// bluemonday's response to a URL it cannot parse is to drop the attribute —
// silently. The element survives with no src, so the image renders as a
// broken icon and its alt text, which reads as "our mail client is broken".
// Nine mails in this mailbox carried such an image and forty-one such a link,
// all from senders who merged a value into a URL without encoding it
// (…&pet_name_string=your pet). Every browser and mail client tolerates that.
//
// Repairing before the policy runs rather than relaxing the policy: encoding
// a space cannot introduce a scheme, an event handler or a script, so this
// only ever narrows what reaches the sanitiser. Tabs and newlines are removed
// rather than encoded because that is what the URL standard says to do with
// them, and because mail HTML is full of line-wrapped attributes — encoding
// those would corrupt links that currently work.
func repairURLWhitespace(s string) string {
	if !strings.Contains(s, "=") {
		return s
	}
	return urlAttr.ReplaceAllStringFunc(s, func(m string) string {
		g := urlAttr.FindStringSubmatch(m)
		quoted := g[3] + g[4] // exactly one of the two groups matched
		if !asciiWhitespaceInURL.MatchString(quoted) {
			return m
		}
		fixed := strings.NewReplacer("\t", "", "\r", "", "\n", "", "\f", "").Replace(quoted)
		fixed = strings.ReplaceAll(fixed, " ", "%20")
		q := `"`
		if g[4] != "" || strings.HasPrefix(g[2], "'") {
			q = `'`
		}
		return g[1] + "=" + q + fixed + q
	})
}

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
	blockEnd = regexp.MustCompile(`(?i)</(p|div|tr|h[1-6]|blockquote|pre)\s*>`)
	// A cell boundary is not nothing. Without this a price row rendered as
	// "货号A1200USD" — three columns with no gap between them, which is worse
	// than losing the table, because the numbers run together into a
	// different number. A tab is the conventional plain-text cell separator
	// and is what copying a table out of a browser produces; the paste parser
	// on the other side of this product splits on it for the same reason.
	// Any trailing tab left before a row break is swept up by trailingWS.
	cellEnd  = regexp.MustCompile(`(?i)</(td|th)\s*>`)
	brTag    = regexp.MustCompile(`(?i)<br\s*/?>`)
	listItem = regexp.MustCompile(`(?i)<li[^>]*>`)
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
	// Cells before rows: </td></tr> has to become "cell<TAB>" then a newline,
	// not a newline followed by a stray tab at the head of the next row.
	s = cellEnd.ReplaceAllString(s, "\t")
	s = blockEnd.ReplaceAllString(s, "\n")
	s = anyTag.ReplaceAllString(s, "")
	s = unescapeEntities(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = trailingWS.ReplaceAllString(s, "\n")
	s = manyBlanks.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// unescapeEntities decodes the whole HTML entity set, not a chosen few.
//
// A handful was enough while this only had to handle markup our own editor
// produced. It now also renders mail written by anybody on the internet, where
// &#847; and &hellip; turn up constantly, and a half-decoded snippet reads as
// a broken product rather than as somebody else's odd markup. html is stdlib,
// so the import costs nothing but the line.
//
// Then the invisibles go. Decoding is not the end of the job: &nbsp; becomes a
// real U+00A0, and bulk senders sprinkle zero-width joiners and combining
// marks through their subject lines to dodge filters. In text they are either
// invisible or, worse, a space that does not behave like one.
func unescapeEntities(s string) string { return invisibleRunes.Replace(html.UnescapeString(s)) }

// StripInvisible removes the characters that are in a mail without being in
// its words.
//
// Exported because the text alternative is no longer the only reader. Search
// indexes the body too, and there these do more than look untidy: a phrase
// with a zero-width joiner dropped into the middle of it does not match a
// query for that phrase, so the mail becomes unfindable by words it visibly
// contains. In one mailbox of 2492 messages, 342 carried zero-width
// characters and 19 carried runs of soft hyphens.
func StripInvisible(s string) string { return invisibleRunes.Replace(s) }

var invisibleRunes = strings.NewReplacer(
	"\u00a0", " ", // non-breaking space — a space, and should wrap like one
	"\u00ad", "", // soft hyphen: bulk senders use runs of it as invisible padding
	"\u034f", "", // combining grapheme joiner
	"\u2060", "", // word joiner, the BOM's non-deprecated sibling
	"\u200b", "", // zero-width space
	"\u200c", "", // zero-width non-joiner
	"\u200d", "", // zero-width joiner
	"\ufeff", "", // byte-order mark, seen mid-string in forwarded mail
)

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

// readerPolicy is what a received mail may keep when it is shown back.
//
// Wider than mailPolicy in exactly two ways — <style> blocks survive, and so
// do class attributes — and narrower in none. Everything dangerous is still
// gone: no script, no event handlers, no javascript: URLs, no iframes, no
// objects.
//
// Those two additions are most of how a modern mail looks like itself. A
// marketing mail is built as a stylesheet plus a scaffold of divs carrying
// class names; strip the stylesheet and the class names dangle, the headings
// fall back to browser defaults, and what arrives on screen is a flat
// approximation that reads as though our client is broken. One mail measured
// here carried ten <style> blocks and seventy-nine class attributes, and its
// display heading — 48px in Gmail — rendered at the browser's default because
// its size lived in a rule we had thrown away.
//
// The policy that removed them justified itself by saying Gmail strips <style>
// too. Gmail does not; the same mail renders in Gmail with its own typography,
// which is what made the difference visible in the first place.
//
// The reason it was ever unsafe is real, though, and is why this is a separate
// policy rather than a widening of the original: the body is injected into our
// own page, so a sender's stylesheet is a stranger writing CSS for our
// application. `body { display: none }` hides the ERP. `position: fixed` puts
// their content over our toolbar, which is a phishing surface. A selector can
// name .el-button and restyle a framework component.
//
// So this output is only ever rendered inside a sandboxed iframe, where the
// sender's CSS cannot reach beyond the document it came in. Sanitising and
// isolating are doing different jobs here and neither replaces the other: the
// sandbox is what makes <style> safe to keep, and the sanitiser is what keeps
// script out even if the sandbox is misconfigured one day.
var readerPolicy = buildReaderPolicy()

func buildReaderPolicy() *bluemonday.Policy {
	p := buildMailPolicy()
	// The stylesheet itself. bluemonday validates the declarations inside.
	p.AllowElements("style")
	p.AllowAttrs("class", "id").Globally()
	// Layout elements real mail uses that the composer never emits, so they
	// are not in the sending whitelist: senders build with these constantly
	// and dropping them collapses the scaffold the CSS is written against.
	p.AllowElements("center", "font", "tbody", "colgroup", "col")
	p.AllowAttrs("bgcolor", "background", "align", "valign").Globally()
	return p
}

// styleBlock captures a stylesheet and its contents.
var styleBlock = regexp.MustCompile(`(?is)<style[^>]*>(.*?)</style>`)

// cssDanger is what is taken out of a kept stylesheet.
//
// A short list, because the frame is doing the containing. @import fetches a
// stylesheet from wherever the sender likes, which is a tracking beacon that
// outlives the image blocker; expression() is IE's way of putting script in a
// declaration and has no business anywhere. Everything else — position, z-index,
// display — is confined to the frame's own document and can be left alone.
var cssDanger = regexp.MustCompile(`(?is)@import\b[^;]*;?|expression\s*\(|javascript\s*:`)

// SanitizeForReading prepares a received mail for display in the reader's
// sandboxed frame. Never use it for anything rendered into our own document.
//
// The stylesheet is lifted out before the policy runs and put back after,
// because bluemonday does not keep it: AllowElements("style") permits the tag
// and the library still discards its contents, by design — it exists for
// user-generated HTML, where a stylesheet is the attack rather than the
// content. Measured on a real marketing mail, the policy alone kept 0 of 10
// stylesheets.
//
// Lifting it out would be reckless on its own. It is only defensible because
// the result is rendered inside a sandboxed frame: there the sender's CSS
// governs their own document and nothing else, so `body { display: none }`
// hides their mail rather than our application, and `position: fixed` cannot
// place anything over our toolbar.
func SanitizeForReading(s string) string {
	s = repairURLWhitespace(s)

	var css strings.Builder
	for _, m := range styleBlock.FindAllStringSubmatch(s, -1) {
		css.WriteString(cssDanger.ReplaceAllString(m[1], ""))
		css.WriteString("\n")
	}
	body := readerPolicy.Sanitize(s)
	if css.Len() == 0 {
		return body
	}
	// Ahead of the body so the mail's own rules are in effect before its
	// markup, and escaped so a stylesheet cannot close its own tag and become
	// markup again.
	return "<style>" + strings.ReplaceAll(css.String(), "</", "<\\/") + "</style>" + body
}
