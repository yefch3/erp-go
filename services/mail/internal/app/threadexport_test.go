package app

import (
	"strings"
	"testing"
	"time"

	// Self-sufficient zones. The service binary embeds these too (see
	// cmd/main.go); importing them here as well means the test proves what it
	// claims on a build machine with no /usr/share/zoneinfo, rather than
	// passing on a developer's Mac and skipping silently in CI.
	_ "time/tzdata"

	"github.com/jackc/pgx/v5/pgtype"
)

func at(s string) pgtype.Timestamptz {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func inbound(text, html string) storeRow {
	return storeRow{Direction: "IN", BodyText: text, BodyHtml: html, BodyFormat: "HTML",
		Counterparty: "buyer@example.com", Who: "Buyer", At: at("2026-03-01T09:00:00Z")}
}

// A local alias so the tests read as prose rather than as a package path.
type storeRow = struct {
	Direction    string
	ID           int64
	Subject      string
	BodyHtml     string
	BodyText     string
	BodyFormat   string
	Counterparty string
	Who          string
	At           pgtype.Timestamptz
}

// The sender's own text half beats our tag-stripper, every time there is one.
// Their mail client knew where the table's columns were; HTMLToText does not.
func TestTheSendersOwnTextPartIsPreferred(t *testing.T) {
	turn := exportTurn(inbound("Price is 12.50 per unit.", "<p>Price is <b>12.50</b> per unit.</p>"), nil)
	if turn.Text != "Price is 12.50 per unit." {
		t.Fatalf("body came from the HTML: %q", turn.Text)
	}
	if turn.Derived {
		t.Fatal("a body taken verbatim was marked as converted")
	}
}

// And when there is no text half, the conversion happens and says so. A
// reader holding a transcript needs to know the tables in it lost their
// columns somewhere.
func TestAnHTMLOnlyBodyIsConvertedAndLabelled(t *testing.T) {
	turn := exportTurn(inbound("", "<p>Hello</p><p>World</p>"), nil)
	if turn.Text != "Hello\nWorld" {
		t.Fatalf("conversion produced %q", turn.Text)
	}
	if !turn.Derived {
		t.Fatal("a converted body was not marked as converted")
	}
}

// An outbound mail composed as plain text must not be run through a tag
// stripper. "if x < y then" is a sentence somebody typed, and anyTag would
// eat from the < to the next >, taking the rest of the line with it.
func TestAPlainTextSendIsNotTagStripped(t *testing.T) {
	row := storeRow{
		Direction: "OUT", BodyFormat: "TEXT",
		BodyHtml: "Ship if x < y then invoice.", At: at("2026-03-01T08:00:00Z"),
	}
	turn := exportTurn(row, nil)
	if turn.Text != "Ship if x < y then invoice." {
		t.Fatalf("plain text was mangled: %q", turn.Text)
	}
	if turn.Derived {
		t.Fatal("plain text was reported as converted")
	}
}

// Truncation is allowed; silent truncation is not. A transcript that stops
// mid-sentence with no mark reads as the message having stopped there.
func TestAnOversizedBodyIsCutAndSaysSo(t *testing.T) {
	long := strings.Repeat("好", exportMaxBodyRunes+500)
	turn := exportTurn(inbound(long, ""), nil)
	if got := len([]rune(turn.Text)); got != exportMaxBodyRunes {
		t.Fatalf("kept %d runes, want %d", got, exportMaxBodyRunes)
	}
	if !turn.Clipped {
		t.Fatal("a truncated body was not marked")
	}
}

// Runes, not bytes. Measuring bytes would cut a Chinese body at a third of
// the length and, worse, could cut one in the middle of a character.
func TestTheBodyLimitCountsCharacters(t *testing.T) {
	turn := exportTurn(inbound(strings.Repeat("好", 2000), ""), nil)
	if turn.Clipped {
		t.Fatal("2000 Chinese characters were treated as oversized")
	}
	if strings.Contains(turn.Text, "�") {
		t.Fatal("the body was cut mid-character")
	}
}

// The heading names the conversation the way a person would: the subject it
// started with, not the one with eight Re: in front, and the address that
// actually carried it rather than whichever one happened to be first.
func TestTheHeadingUsesTheFirstSubjectAndTheCommonestAddress(t *testing.T) {
	turns := []ExportTurn{
		{Subject: "Enquiry: 500 units", Address: "sales@buyer.com"},
		{Subject: "Re: Enquiry: 500 units", Address: "ana@buyer.com"},
		{Subject: "Re: Re: Enquiry: 500 units", Address: "ana@buyer.com"},
	}
	subject, counterparty := threadHeading(turns)
	if subject != "Enquiry: 500 units" {
		t.Fatalf("subject = %q", subject)
	}
	if counterparty != "ana@buyer.com" {
		t.Fatalf("counterparty = %q", counterparty)
	}
}

// A conversation with no subject anywhere still has to produce a file with a
// name on it.
func TestAThreadWithNoSubjectStillGetsAFileName(t *testing.T) {
	doc := renderTranscript(ThreadExport{
		Turns:      []ExportTurn{{Direction: "IN", Text: "..."}},
		ExportedAt: time.Unix(1780000000, 0),
	})
	if doc.FileName == "" || doc.FileName == ".html" {
		t.Fatalf("file name = %q", doc.FileName)
	}
}

// ------------------------------------------------------------ the document

func renderedFor(t *testing.T, ex ThreadExport) string {
	t.Helper()
	return string(renderTranscript(ex).Content)
}

// The one property that makes this file safe to hand to somebody outside the
// company: opening it does nothing. No stylesheet, no script, no image, no
// request of any kind — which also means none of the sender's tracking
// pixels fire on the recipient's laptop, weeks later, telling them the mail
// was read again.
func TestTheDocumentFetchesNothing(t *testing.T) {
	doc := renderedFor(t, ThreadExport{
		Subject:      "Quote",
		Counterparty: "buyer@example.com",
		Turns: []ExportTurn{{
			Direction: "IN", Who: "Buyer", Address: "buyer@example.com",
			Text: "See https://example.com/price and the attached file.",
		}},
	})
	for _, forbidden := range []string{"<script", "<img", "<link", "<iframe", "src=", "@import", "url("} {
		if strings.Contains(strings.ToLower(doc), forbidden) {
			t.Fatalf("the document contains %q", forbidden)
		}
	}
}

// A URL in the body stays text. It is evidence of what was written, not
// something to click, and turning it into a link would put a live outbound
// reference back into a document whose whole point is not having any.
func TestALinkInTheBodyStaysText(t *testing.T) {
	doc := renderedFor(t, ThreadExport{
		Turns: []ExportTurn{{Direction: "IN", Text: "http://phish.example/pay-here"}},
	})
	if strings.Contains(doc, "<a ") || strings.Contains(doc, "href=") {
		t.Fatal("a body URL became a link")
	}
	if !strings.Contains(doc, "http://phish.example/pay-here") {
		t.Fatal("the URL was lost from the transcript")
	}
}

// Every field in this document came from somebody outside the company: the
// subject line, the display name, the file names. All of it is escaped, and
// the test checks the ones a sender chooses rather than the one we compose.
func TestSenderControlledTextIsEscaped(t *testing.T) {
	doc := renderedFor(t, ThreadExport{
		Subject: `<script>alert(1)</script>`,
		Turns: []ExportTurn{{
			Direction: "IN",
			Who:       `<b>Buyer</b>`,
			Address:   `"><script>x</script>@evil.example`,
			Text:      `</pre><script>y</script>`,
			Files:     []ExportFile{{Name: `<img onerror=z>.pdf`, Size: 10}},
		}},
	})
	// The tags themselves, not the words inside them: "onerror=z" sitting in
	// an escaped file name is text on a page, and asserting against the word
	// would fail on a document that is perfectly safe.
	for _, tag := range []string{"<script", "<img", "<b>"} {
		if strings.Contains(doc, tag) {
			t.Fatalf("unescaped %s survived:\n%s", tag, doc)
		}
	}
	if !strings.Contains(doc, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatal("the subject was not escaped into the document")
	}
	// And the <pre> a sender tried to close from inside their own body is
	// still one element, not two.
	if strings.Count(doc, "</pre>") != 1 {
		t.Fatal("a body closed its own <pre>")
	}
}

// The document is the print master, so it has to carry print rules. Without
// @page the browser's default margins and the browser's default header —
// the URL of the page it printed — become part of the record.
func TestTheDocumentCarriesPrintRules(t *testing.T) {
	doc := renderedFor(t, ThreadExport{Turns: []ExportTurn{{Direction: "IN", Text: "x"}}})
	for _, want := range []string{"@page", "@media print"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("the document has no %s rule", want)
		}
	}
}

// Three languages, three documents. The labels the document writes for itself
// follow the exporter, because they are about to hand it to somebody who
// reads what they read.
func TestTheDocumentSpeaksTheExportersLanguage(t *testing.T) {
	for lang, want := range map[string]string{"zh": "导出人", "en": "Exported by", "es": "Exportado por"} {
		doc := renderedFor(t, ThreadExport{
			Lang: lang, ExportedBy: "张三",
			Turns: []ExportTurn{{Direction: "IN", Text: "x"}},
		})
		if !strings.Contains(doc, want) {
			t.Fatalf("the %s document does not say %q", lang, want)
		}
	}
}

// An unknown locale must produce a document, not an empty one. Chinese is the
// fallback because it is the working language here.
func TestAnUnknownLanguageFallsBackRatherThanBlank(t *testing.T) {
	doc := renderedFor(t, ThreadExport{Lang: "fr", Turns: []ExportTurn{{Direction: "IN", Text: "x"}}})
	if !strings.Contains(doc, "收到") {
		t.Fatal("an unknown locale produced a document with no labels")
	}
}

// ------------------------------------------------------------- times, names

// The offset is printed beside the time. Six months later in another country
// "14:32" alone cannot answer "when did they actually reply", and a zone the
// server failed to load would otherwise shift the whole record silently.
func TestTimestampsCarryTheirOffset(t *testing.T) {
	shanghai := loadZone("Asia/Shanghai")
	if shanghai == time.UTC {
		t.Fatal("Asia/Shanghai did not resolve; the zone database is missing")
	}
	got := stamp(time.Date(2026, 3, 1, 1, 30, 0, 0, time.UTC), shanghai, labelsFor("zh"))
	if got != "2026-03-01 09:30 +08:00" {
		t.Fatalf("stamp = %q", got)
	}
}

func TestAnUnknownZoneFallsBackToUTCRatherThanFailing(t *testing.T) {
	if loadZone("Mars/Olympus") != time.UTC {
		t.Fatal("a nonsense zone did not fall back to UTC")
	}
	if loadZone("") != time.UTC {
		t.Fatal("an absent zone did not fall back to UTC")
	}
}

// The file name is built from an address anybody can choose for themselves,
// and it ends up in a Content-Disposition header and on a filesystem.
func TestTheFileNameCannotEscapeItsHeaderOrItsFolder(t *testing.T) {
	stem := transcriptStem(ThreadExport{
		Counterparty: "../../etc/passwd\r\nX-Evil: 1",
		ExportedAt:   time.Unix(1780000000, 0),
	})
	for _, bad := range []string{"/", "\\", "\r", "\n", ":"} {
		if strings.Contains(stem, bad) {
			t.Fatalf("file stem %q still contains %q", stem, bad)
		}
	}
}

func TestTheFileNameIsBounded(t *testing.T) {
	stem := transcriptStem(ThreadExport{
		Counterparty: strings.Repeat("长", 400), ExportedAt: time.Unix(1780000000, 0),
	})
	if len(stem) > 160 {
		t.Fatalf("file stem is %d bytes", len(stem))
	}
}

func TestHumanBytesReadsLikeASize(t *testing.T) {
	for in, want := range map[int64]string{
		0: "0 B", 512: "512 B", 1024: "1.0 KB", 319488: "312 KB",
		1468006: "1.4 MB", 1073741824: "1.0 GB",
	} {
		if got := humanBytes(in); got != want {
			t.Fatalf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
