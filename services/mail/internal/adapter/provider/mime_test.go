package provider

import (
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
