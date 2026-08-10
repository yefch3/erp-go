package provider

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/app"
)

// buildMessage renders one Outbound into RFC 5322 bytes.
//
// Structure depends on what is actually present, because empty containers
// confuse some clients more than they help:
//
//	text only, no files      -> text/plain
//	html, no files           -> multipart/alternative
//	html with inline images  -> multipart/related wrapping the alternative
//	anything with files      -> multipart/mixed wrapping whichever of the above
//
// The related layer exists so a signature logo travels *with* the message
// instead of being fetched from us afterwards. Remote images are blocked by
// default in Outlook and Thunderbird and proxied by Gmail, so a linked logo is
// one nobody reliably sees; an inline part raises no such question, because
// looking at it tells the sender nothing. It also removes the dependency on a
// publicly reachable address altogether.
//
// The open pixel is deliberately *not* inlined: it works precisely because the
// recipient has to come and fetch it. It stays an absolute URL.
func buildMessage(m app.Outbound, fromEmail, domain string, files []fileBlob, now time.Time) ([]byte, string, error) {
	messageID := fmt.Sprintf("<%s@%s>", m.MessageKey, domain)

	var buf bytes.Buffer
	h := func(k, v string) { fmt.Fprintf(&buf, "%s: %s\r\n", k, v) }

	h("From", addressHeader(m.FromName, fromEmail))
	// A merged send lists everybody openly; the normal send names exactly one
	// person. Which one this is was decided at queue time, not here.
	if len(m.ToList) > 0 {
		h("To", addressListHeader(m.ToList))
	} else {
		h("To", addressHeader(m.ToName, m.ToEmail))
	}
	if len(m.CCList) > 0 {
		h("Cc", addressListHeader(m.CCList))
	}
	h("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	h("Date", now.Format(time.RFC1123Z))
	h("Message-ID", messageID)
	// Threading. In-Reply-To names the mail being answered; References is the
	// whole chain. They are what makes the other side's client stack this
	// answer under the question instead of starting a new conversation.
	if m.InReplyTo != "" {
		h("In-Reply-To", m.InReplyTo)
	}
	if m.References != "" {
		h("References", strings.Join(strings.Fields(m.References), "\r\n "))
	}
	h("MIME-Version", "1.0")
	// Bulk mail that cannot be unsubscribed from is what gets a domain
	// listed. There is no unsubscribe endpoint yet, so this says what is
	// true rather than pointing at something that does not exist.
	if m.Format == "HTML" {
		h("X-Auto-Response-Suppress", "OOF, AutoReply")
	}

	switch {
	case len(files) > 0:
		// The writer is created before the header is written so the boundary
		// in the header and the one in the body cannot drift apart.
		var body bytes.Buffer
		mixed := multipart.NewWriter(&body)
		h("Content-Type", "multipart/mixed; boundary="+mixed.Boundary())
		buf.WriteString("\r\n")

		if err := writeBodyPart(mixed, m); err != nil {
			return nil, "", err
		}
		for _, f := range files {
			if err := writeFilePart(mixed, f); err != nil {
				return nil, "", err
			}
		}
		if err := mixed.Close(); err != nil {
			return nil, "", err
		}
		buf.Write(body.Bytes())

	case m.Format == "HTML":
		ct, body, err := buildBodyBlock(m)
		if err != nil {
			return nil, "", err
		}
		h("Content-Type", ct)
		buf.WriteString("\r\n")
		buf.Write(body)

	default:
		h("Content-Type", `text/plain; charset="utf-8"`)
		h("Content-Transfer-Encoding", "base64")
		buf.WriteString("\r\n")
		buf.WriteString(wrapBase64([]byte(m.Body)))
	}

	return buf.Bytes(), messageID, nil
}

// writeBodyPart puts the message body inside a multipart/mixed, keeping the
// alternative structure intact when there is one.
func writeBodyPart(w *multipart.Writer, m app.Outbound) error {
	if m.Format != "HTML" {
		p, err := w.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {`text/plain; charset="utf-8"`},
			"Content-Transfer-Encoding": {"base64"},
		})
		if err != nil {
			return err
		}
		_, err = p.Write([]byte(wrapBase64([]byte(m.Body))))
		return err
	}

	ct, inner, err := buildBodyBlock(m)
	if err != nil {
		return err
	}
	p, err := w.CreatePart(textproto.MIMEHeader{"Content-Type": {ct}})
	if err != nil {
		return err
	}
	_, err = p.Write(inner)
	return err
}

// buildBodyBlock renders the body and whatever must stay glued to it, and
// reports the Content-Type its container has to declare.
//
// One function for both callers — the top-level HTML case and the one nested
// inside a multipart/mixed — because the alternative-versus-related decision
// is the same decision in both places, and having made it twice is how the two
// drift apart.
func buildBodyBlock(m app.Outbound) (string, []byte, error) {
	var alternative bytes.Buffer
	alt := multipart.NewWriter(&alternative)
	if err := writeAlternative(alt, m); err != nil {
		return "", nil, err
	}
	if err := alt.Close(); err != nil {
		return "", nil, err
	}
	altType := "multipart/alternative; boundary=" + alt.Boundary()
	if len(m.InlineImages) == 0 {
		return altType, alternative.Bytes(), nil
	}

	// related, with the alternative as its root part: the pictures are not
	// alternatives to the text, they are pieces of it.
	var related bytes.Buffer
	rel := multipart.NewWriter(&related)
	root, err := rel.CreatePart(textproto.MIMEHeader{"Content-Type": {altType}})
	if err != nil {
		return "", nil, err
	}
	if _, err := root.Write(alternative.Bytes()); err != nil {
		return "", nil, err
	}
	for _, img := range m.InlineImages {
		if err := writeInlinePart(rel, img); err != nil {
			return "", nil, err
		}
	}
	if err := rel.Close(); err != nil {
		return "", nil, err
	}
	// type= names the root part. RFC 2387 asks for it; Gmail omits it and
	// clients cope either way, but saying which part is the document rather
	// than leaving a reader to guess costs nothing.
	return `multipart/related; type="multipart/alternative"; boundary=` + rel.Boundary(),
		related.Bytes(), nil
}

// writeInlinePart writes a picture the body refers to by Content-ID.
//
// Angle brackets around the identifier are not decoration: RFC 2392 defines
// cid: as referring to the Content-ID *without* them, and a reader that takes
// the header literally will not match "cid:abc" against "abc" if the header
// said "<abc>" — or, worse, will match neither way if they are missing here.
func writeInlinePart(w *multipart.Writer, img app.InlineImage) error {
	ct := img.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	disp := mime.FormatMediaType("inline", map[string]string{"filename": img.FileName})
	p, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {ct},
		"Content-Transfer-Encoding": {"base64"},
		"Content-ID":                {"<" + img.ContentID + ">"},
		"Content-Disposition":       {disp},
	})
	if err != nil {
		return err
	}
	_, err = p.Write([]byte(wrapBase64(img.Data)))
	return err
}

// writeAlternative writes text then HTML, in that order.
//
// The order is load-bearing: a client picks the last part it understands, so
// text first and HTML second is what makes an HTML-capable reader show the
// HTML. Reversed, everybody gets plain text.
func writeAlternative(w *multipart.Writer, m app.Outbound) error {
	text := m.BodyText
	if strings.TrimSpace(text) == "" {
		// Sending HTML with no text alternative is a measurable spam signal,
		// so there is always something here even if the renderer gave us
		// nothing to work with.
		text = "This message is best viewed in an HTML-capable mail reader."
	}
	p, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {`text/plain; charset="utf-8"`},
		"Content-Transfer-Encoding": {"base64"},
	})
	if err != nil {
		return err
	}
	if _, err := p.Write([]byte(wrapBase64([]byte(text)))); err != nil {
		return err
	}

	p, err = w.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {`text/html; charset="utf-8"`},
		"Content-Transfer-Encoding": {"base64"},
	})
	if err != nil {
		return err
	}
	_, err = p.Write([]byte(wrapBase64([]byte(m.Body))))
	return err
}

type fileBlob struct {
	FileName    string
	ContentType string
	Data        []byte
}

func writeFilePart(w *multipart.Writer, f fileBlob) error {
	ct := f.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	// RFC 2231 for the filename, so a Chinese or accented attachment name
	// survives instead of arriving as "ATT00001.dat".
	disp := mime.FormatMediaType("attachment", map[string]string{"filename": f.FileName})
	p, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {ct},
		"Content-Transfer-Encoding": {"base64"},
		"Content-Disposition":       {disp},
	})
	if err != nil {
		return err
	}
	_, err = p.Write([]byte(wrapBase64(f.Data)))
	return err
}

// addressListHeader folds one address per line. The fold is not cosmetic:
// fifty addresses on one physical line would blow the 998-byte limit and get
// truncated by stricter hosts.
func addressListHeader(list []app.NamedAddress) string {
	parts := make([]string, 0, len(list))
	for _, a := range list {
		parts = append(parts, addressHeader(a.Name, a.Email))
	}
	return strings.Join(parts, ",\r\n ")
}

// addressHeader renders a display name safely. A name is only ever emitted
// RFC 2047-encoded or quoted, so a comma or a newline in somebody's name
// cannot inject a header or a second recipient.
func addressHeader(name, email string) string {
	name = strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(name))
	if name == "" {
		return "<" + email + ">"
	}
	return mime.QEncoding.Encode("utf-8", name) + " <" + email + ">"
}

// wrapBase64 encodes and folds at 76 characters. Lines longer than 998 bytes
// are illegal in SMTP and some hosts silently truncate well before that.
func wrapBase64(b []byte) string {
	const width = 76
	enc := base64.StdEncoding.EncodeToString(b)
	var out strings.Builder
	for len(enc) > width {
		out.WriteString(enc[:width])
		out.WriteString("\r\n")
		enc = enc[width:]
	}
	out.WriteString(enc)
	out.WriteString("\r\n")
	return out.String()
}
