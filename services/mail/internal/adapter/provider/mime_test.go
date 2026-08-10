package provider

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/app"
)

var (
	fixedTime = time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	b64       = base64.StdEncoding
)

// partOrder returns the media types of a multipart's direct children, in the
// order they appear on the wire — which is what decides what a reader shows.
func partOrder(t *testing.T, ctype, body string) []string {
	t.Helper()
	_, params, err := mime.ParseMediaType(ctype)
	if err != nil {
		t.Fatalf("bad Content-Type %q: %v", ctype, err)
	}
	mr := multipart.NewReader(strings.NewReader(body), params["boundary"])
	var out []string
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("reading part: %v", err)
		}
		mt, _, err := mime.ParseMediaType(p.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("part has an unparseable Content-Type: %v", err)
		}
		out = append(out, mt)
	}
	return out
}

func parse(t *testing.T, raw []byte) *mail.Message {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("the message is not parseable as RFC 5322: %v", err)
	}
	return msg
}

// Walks the MIME tree and returns every leaf as contentType -> decoded body.
func leaves(t *testing.T, ctype string, body io.Reader) map[string]string {
	t.Helper()
	out := map[string]string{}
	mt, params, err := mime.ParseMediaType(ctype)
	if err != nil {
		t.Fatalf("bad Content-Type %q: %v", ctype, err)
	}
	if !strings.HasPrefix(mt, "multipart/") {
		b, _ := io.ReadAll(body)
		out[mt] = decode(string(b))
		return out
	}
	mr := multipart.NewReader(body, params["boundary"])
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("reading part: %v", err)
		}
		sub := leaves(t, p.Header.Get("Content-Type"), p)
		for k, v := range sub {
			if _, dup := out[k]; dup {
				k += "#2"
			}
			out[k] = v
		}
	}
	return out
}

func decode(s string) string {
	dec := strings.NewReplacer("\r\n", "", "\n", "").Replace(s)
	b, err := b64.DecodeString(dec)
	if err != nil {
		return s
	}
	return string(b)
}

func TestPlainTextMessageIsWellFormed(t *testing.T) {
	m := app.Outbound{
		MessageKey: "k-1", FromName: "李娜", ToName: "Klaus",
		ToEmail: "klaus@example.com", Subject: "报价单 Q3", Body: "正文内容",
		Format: "TEXT",
	}
	raw, id, err := buildMessage(m, "lina@sunrise.com", "sunrise.com", nil, fixedTime)
	if err != nil {
		t.Fatal(err)
	}
	msg := parse(t, raw)

	if id != "<k-1@sunrise.com>" {
		t.Fatalf("Message-ID should follow the key@domain convention, got %q", id)
	}
	if got := msg.Header.Get("Message-ID"); got != id {
		t.Fatalf("header %q does not match returned id %q", got, id)
	}
	if !strings.HasPrefix(msg.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("text-only mail should be text/plain, got %q", msg.Header.Get("Content-Type"))
	}

	body, _ := io.ReadAll(msg.Body)
	if decode(string(body)) != "正文内容" {
		t.Fatalf("body did not survive the round trip: %q", decode(string(body)))
	}
}

// A non-ASCII subject must be encoded, not passed through raw — a raw UTF-8
// subject is what produces mojibake in Outlook.
func TestNonAsciiSubjectAndNameAreEncoded(t *testing.T) {
	m := app.Outbound{
		MessageKey: "k-2", FromName: "李娜", ToEmail: "k@example.com",
		Subject: "报价单 Q3", Body: "x", Format: "TEXT",
	}
	raw, _, _ := buildMessage(m, "lina@sunrise.com", "sunrise.com", nil, fixedTime)
	msg := parse(t, raw)

	rawSubject := msg.Header.Get("Subject")
	if strings.Contains(rawSubject, "报价单") {
		t.Fatal("subject went out as raw UTF-8 instead of being encoded")
	}
	got, err := new(mime.WordDecoder).DecodeHeader(rawSubject)
	if err != nil || got != "报价单 Q3" {
		t.Fatalf("subject did not decode back: %q (%v)", got, err)
	}

	from, err := mail.ParseAddress(msg.Header.Get("From"))
	if err != nil {
		t.Fatalf("From is not a parseable address: %v", err)
	}
	if from.Name != "李娜" || from.Address != "lina@sunrise.com" {
		t.Fatalf("From did not round trip: %+v", from)
	}
}

// The order inside multipart/alternative decides what people see: a reader
// picks the LAST part it understands, so HTML must come second.
func TestHtmlMailCarriesTextAlternativeFirst(t *testing.T) {
	m := app.Outbound{
		MessageKey: "k-3", ToEmail: "k@example.com", Subject: "s",
		Body: "<p>Hello <b>Klaus</b></p>", BodyText: "Hello Klaus", Format: "HTML",
	}
	raw, _, _ := buildMessage(m, "lina@sunrise.com", "sunrise.com", nil, fixedTime)
	msg := parse(t, raw)

	ct := msg.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "multipart/alternative") {
		t.Fatalf("want multipart/alternative, got %q", ct)
	}

	body, _ := io.ReadAll(msg.Body)
	order := partOrder(t, ct, string(body))
	if len(order) != 2 || order[0] != "text/plain" || order[1] != "text/html" {
		t.Fatalf("alternative parts must be text then html, got %v", order)
	}

	got := leaves(t, ct, strings.NewReader(string(body)))
	if got["text/html"] != "<p>Hello <b>Klaus</b></p>" {
		t.Fatalf("html part wrong: %q", got["text/html"])
	}
	if got["text/plain"] != "Hello Klaus" {
		t.Fatalf("text part wrong: %q", got["text/plain"])
	}
}

// HTML with no text alternative is a measurable spam signal, so one is
// always synthesised rather than sending a bare HTML part.
func TestHtmlWithoutTextStillGetsAnAlternative(t *testing.T) {
	m := app.Outbound{
		MessageKey: "k-4", ToEmail: "k@example.com", Subject: "s",
		Body: "<p>Hi</p>", BodyText: "   ", Format: "HTML",
	}
	raw, _, _ := buildMessage(m, "lina@sunrise.com", "sunrise.com", nil, fixedTime)
	msg := parse(t, raw)
	got := leaves(t, msg.Header.Get("Content-Type"), msg.Body)

	if strings.TrimSpace(got["text/plain"]) == "" {
		t.Fatal("no text alternative was produced")
	}
}

func TestAttachmentsWrapTheBodyInMixed(t *testing.T) {
	m := app.Outbound{
		MessageKey: "k-5", ToEmail: "k@example.com", Subject: "s",
		Body: "<p>see attached</p>", BodyText: "see attached", Format: "HTML",
	}
	files := []fileBlob{{
		FileName: "报价单 Q3.pdf", ContentType: "application/pdf", Data: []byte("%PDF-1.4 fake"),
	}}
	raw, _, err := buildMessage(m, "lina@sunrise.com", "sunrise.com", files, fixedTime)
	if err != nil {
		t.Fatal(err)
	}
	msg := parse(t, raw)

	ct := msg.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "multipart/mixed") {
		t.Fatalf("want multipart/mixed, got %q", ct)
	}
	got := leaves(t, ct, msg.Body)
	if got["text/html"] != "<p>see attached</p>" {
		t.Fatalf("html body lost inside mixed: %q", got["text/html"])
	}
	if got["application/pdf"] != "%PDF-1.4 fake" {
		t.Fatalf("attachment bytes did not survive: %q", got["application/pdf"])
	}
}

// A Chinese filename must arrive intact rather than as ATT00001.dat.
func TestAttachmentFilenameSurvives(t *testing.T) {
	m := app.Outbound{MessageKey: "k-6", ToEmail: "k@example.com", Subject: "s", Body: "b", Format: "TEXT"}
	files := []fileBlob{{FileName: "报价单 Q3.pdf", ContentType: "application/pdf", Data: []byte("x")}}
	raw, _, _ := buildMessage(m, "lina@sunrise.com", "sunrise.com", files, fixedTime)
	msg := parse(t, raw)

	_, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var names []string
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if d := p.Header.Get("Content-Disposition"); strings.HasPrefix(d, "attachment") {
			_, dp, err := mime.ParseMediaType(d)
			if err != nil {
				t.Fatalf("Content-Disposition is not parseable: %v", err)
			}
			names = append(names, dp["filename"])
		}
	}
	if len(names) != 1 || names[0] != "报价单 Q3.pdf" {
		t.Fatalf("filename did not survive: %v", names)
	}
}

// A newline in a display name would otherwise let somebody inject a header
// or a second recipient.
func TestNameWithNewlineCannotInjectHeaders(t *testing.T) {
	m := app.Outbound{
		MessageKey: "k-7", FromName: "Evil\r\nBcc: victim@example.com",
		ToEmail: "k@example.com", Subject: "s", Body: "b", Format: "TEXT",
	}
	raw, _, err := buildMessage(m, "lina@sunrise.com", "sunrise.com", nil, fixedTime)
	if err != nil {
		t.Fatal(err)
	}
	msg := parse(t, raw)
	if msg.Header.Get("Bcc") != "" {
		t.Fatal("a newline in the display name injected a Bcc header")
	}
}

// Lines over 998 bytes are illegal in SMTP; some hosts truncate well before.
func TestNoLineExceedsTheSmtpLimit(t *testing.T) {
	m := app.Outbound{
		MessageKey: "k-8", ToEmail: "k@example.com", Subject: "s",
		Body: strings.Repeat("这是一段很长的正文，用来撑爆行长度限制。", 300), Format: "TEXT",
	}
	raw, _, _ := buildMessage(m, "lina@sunrise.com", "sunrise.com", nil, fixedTime)
	for i, line := range strings.Split(string(raw), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line %d is %d bytes, over the 998 limit", i, len(line))
		}
	}
}

func TestBuildMessageNeverWritesABccHeader(t *testing.T) {
	// The whole meaning of a blind copy is that the other recipients cannot
	// see it. A Bcc header in the delivered bytes would tell all of them, so
	// this asserts the absence of something rather than the presence — which
	// is exactly the kind of property a later edit removes without noticing.
	raw, _, err := buildMessage(app.Outbound{
		MessageKey: "k", Subject: "Q3 offer", Body: "hello", Format: "TEXT",
		ToList:  []app.NamedAddress{{Name: "Hans", Email: "hans@acme.de"}},
		CCList:  []app.NamedAddress{{Name: "Mike", Email: "mike@pacific.com"}},
		BCCList: []app.NamedAddress{{Name: "Boss", Email: "boss@ourcompany.cn"}},
	}, "me@ourcompany.cn", "ourcompany.cn", nil, time.Now())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got := string(raw)
	if strings.Contains(strings.ToLower(got), "bcc:") {
		t.Error("a Bcc header reached the delivered message")
	}
	if strings.Contains(got, "boss@ourcompany.cn") {
		t.Error("the blind address appears in the message bytes; it belongs only in the envelope")
	}
	// The visible cast must still be visible, or the test would pass on a
	// build that dropped every header.
	if !strings.Contains(got, "hans@acme.de") || !strings.Contains(got, "mike@pacific.com") {
		t.Error("To/Cc lost their recipients")
	}
}

// A forwarded original is attached as message/rfc822, which is what makes a
// recipient's client offer to open it as a message rather than hand them an
// opaque blob to save. The whole reason to forward as an attachment is that
// the original's own headers survive, and they only survive if the part is
// typed as a message.
//
// The bytes are base64, which RFC 2046 §5.2.1 reads as disallowed for
// message/* — it permits only 7bit, 8bit and binary. Base64 is chosen anyway,
// deliberately: archived MIME is arbitrary bytes that may carry lines past
// the 998-octet SMTP limit or 8-bit content the next hop will not take, and
// inlining it risks a mangled message rather than a rejected one. It is also
// what Gmail and Outlook emit for the same feature, so receivers handle it.
func TestForwardedOriginalIsAttachedAsAMessage(t *testing.T) {
	original := "From: customer@example.com\r\n" +
		"Subject: Original\r\n" +
		"Message-ID: <abc@example.com>\r\n\r\n" +
		"the words the customer actually wrote\r\n"

	m := app.Outbound{
		MessageKey: "k-eml", ToEmail: "colleague@example.com", Subject: "Fwd: Original",
		Body: "<p>see attached</p>", BodyText: "see attached", Format: "HTML",
	}
	files := []fileBlob{{
		FileName: "Original.eml", ContentType: "message/rfc822", Data: []byte(original),
	}}
	raw, _, err := buildMessage(m, "lina@sunrise.com", "sunrise.com", files, fixedTime)
	if err != nil {
		t.Fatal(err)
	}
	msg := parse(t, raw)

	got := leaves(t, msg.Header.Get("Content-Type"), msg.Body)
	body, ok := got["message/rfc822"]
	if !ok {
		t.Fatalf("the original was not attached as message/rfc822; parts present: %v", keysOf(got))
	}
	// Byte for byte: a forward that re-encoded the original would defeat the
	// point, since the headers are what somebody forwards it for.
	if body != original {
		t.Fatalf("the original did not survive intact:\n got %q\nwant %q", body, original)
	}
	if !strings.Contains(body, "Message-ID: <abc@example.com>") {
		t.Error("the original's Message-ID is missing, so the copy cannot be traced to the mail it claims to be")
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A signature logo must travel with the message, not be fetched from us
// afterwards. Remote images are blocked by default in Outlook and Thunderbird
// and proxied by Gmail, so a linked logo is one nobody reliably sees — and the
// link only works at all once a publicly reachable address exists.
func TestInlineImagesTravelInsideTheMessage(t *testing.T) {
	m := app.Outbound{
		MessageKey: "k1", Subject: "s", ToEmail: "b@example.com", Format: "HTML",
		Body:     `<p>hi</p><img src="cid:tok123">`,
		BodyText: "hi",
		InlineImages: []app.InlineImage{{
			ContentID: "tok123", FileName: "image.png",
			ContentType: "image/png", Data: []byte{1, 2, 3, 4},
		}},
	}
	raw, _, err := buildMessage(m, "a@example.com", "example.com", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)

	if !strings.Contains(got, "multipart/related") {
		t.Error("no related layer, so the picture is not glued to the body")
	}
	if !strings.Contains(got, `type="multipart/alternative"`) {
		t.Error("related does not name its root part")
	}
	// The angle brackets matter: cid:tok123 refers to Content-ID <tok123>.
	if !strings.Contains(got, "Content-ID: <tok123>") {
		t.Error("the picture carries no identifier the body can reach")
	}
	if !strings.Contains(got, "Content-Disposition: inline") {
		t.Error("the picture is not marked inline")
	}
	// The alternative must survive inside the related wrapper, in that order.
	rel := strings.Index(got, "multipart/related")
	alt := strings.Index(got, "multipart/alternative")
	if rel < 0 || alt < 0 || alt < rel {
		t.Errorf("the alternative is not nested inside the related part (rel=%d alt=%d)", rel, alt)
	}
}

// Nothing inline: the shape must be exactly what it was before, or every mail
// without a logo pays for a feature it is not using.
func TestWithoutInlineImagesTheShapeIsUnchanged(t *testing.T) {
	m := app.Outbound{
		MessageKey: "k1", Subject: "s", ToEmail: "b@example.com", Format: "HTML",
		Body: "<p>hi</p>", BodyText: "hi",
	}
	raw, _, err := buildMessage(m, "a@example.com", "example.com", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if strings.Contains(got, "multipart/related") {
		t.Error("a related layer appeared with nothing to relate")
	}
	if !strings.Contains(got, "multipart/alternative") {
		t.Error("the alternative went missing")
	}
}

// Attachments and inline images together: the files wrap everything, and the
// picture stays glued to the body inside that.
func TestAttachmentsAndInlineImagesNestCorrectly(t *testing.T) {
	m := app.Outbound{
		MessageKey: "k1", Subject: "s", ToEmail: "b@example.com", Format: "HTML",
		Body: `<img src="cid:tok123">`, BodyText: "hi",
		InlineImages: []app.InlineImage{{
			ContentID: "tok123", FileName: "image.png",
			ContentType: "image/png", Data: []byte{1, 2, 3},
		}},
	}
	files := []fileBlob{{FileName: "quote.pdf", ContentType: "application/pdf", Data: []byte("pdf")}}
	raw, _, err := buildMessage(m, "a@example.com", "example.com", files, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	mixed := strings.Index(got, "multipart/mixed")
	rel := strings.Index(got, "multipart/related")
	alt := strings.Index(got, "multipart/alternative")
	if mixed < 0 || rel < 0 || alt < 0 {
		t.Fatalf("a layer is missing (mixed=%d rel=%d alt=%d)", mixed, rel, alt)
	}
	if !(mixed < rel && rel < alt) {
		t.Errorf("wrong nesting order: mixed=%d rel=%d alt=%d", mixed, rel, alt)
	}
	if !strings.Contains(got, "quote.pdf") {
		t.Error("the attachment went missing")
	}
	if !strings.Contains(got, "Content-ID: <tok123>") {
		t.Error("the inline picture went missing")
	}
}

// The send side and the receive side have to agree. Building a message with an
// inline picture and reading it back with our own parser is the only check
// that covers both at once — a Content-ID we write but cannot find again would
// pass every structural assertion above and still arrive as an empty box.
//
// This is the exact shape that arrived broken from Gmail before the parser was
// fixed: an image part with no filename, identified only by Content-ID.
func TestAnInlineImageSurvivesOurOwnRoundTrip(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3}
	m := app.Outbound{
		MessageKey: "k1", Subject: "报价", ToEmail: "b@example.com", Format: "HTML",
		Body:     `<p>见下图</p><img src="cid:tok123" alt="logo">`,
		BodyText: "见下图",
		InlineImages: []app.InlineImage{{
			ContentID: "tok123", FileName: "image.png",
			ContentType: "image/png", Data: png,
		}},
	}
	raw, _, err := buildMessage(m, "a@example.com", "example.com", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := app.ParseMail(raw)
	if err != nil {
		t.Fatalf("we cannot read back what we just wrote: %v", err)
	}
	if len(parsed.Attachments) != 1 {
		t.Fatalf("the picture did not survive: %d parts", len(parsed.Attachments))
	}
	a := parsed.Attachments[0]
	if a.ContentID != "tok123" {
		t.Errorf("ContentID came back as %q, so the body's cid: finds nothing", a.ContentID)
	}
	if !bytes.Equal(a.Data, png) {
		t.Errorf("the bytes changed in transit: sent %v, got %v", png, a.Data)
	}
	if !strings.Contains(parsed.BodyHTML, "cid:tok123") {
		t.Errorf("the body lost its reference: %q", parsed.BodyHTML)
	}
	// And the text alternative still has to be there — HTML alone is a spam
	// signal, and the related layer must not have swallowed it.
	if !strings.Contains(parsed.BodyText, "见下图") {
		t.Errorf("the plain-text alternative went missing: %q", parsed.BodyText)
	}
}
