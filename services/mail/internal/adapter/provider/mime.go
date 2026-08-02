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
//	anything with files      -> multipart/mixed wrapping the above
//
// Inline images are already absolute URLs by the time they reach here (they
// are served from the public image route), so there is no multipart/related
// layer and no Content-ID juggling.
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
		var body bytes.Buffer
		alt := multipart.NewWriter(&body)
		h("Content-Type", "multipart/alternative; boundary="+alt.Boundary())
		buf.WriteString("\r\n")
		if err := writeAlternative(alt, m); err != nil {
			return nil, "", err
		}
		if err := alt.Close(); err != nil {
			return nil, "", err
		}
		buf.Write(body.Bytes())

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

	var inner bytes.Buffer
	alt := multipart.NewWriter(&inner)
	if err := writeAlternative(alt, m); err != nil {
		return err
	}
	if err := alt.Close(); err != nil {
		return err
	}
	p, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Type": {"multipart/alternative; boundary=" + alt.Boundary()},
	})
	if err != nil {
		return err
	}
	_, err = p.Write(inner.Bytes())
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
