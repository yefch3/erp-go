package app

import (
	"bytes"
	"io"
	"mime"
	netmail "net/mail"
	"strings"
	"time"

	emsg "github.com/emersion/go-message"
	// Registers GB2312, GBK, Big5, Shift-JIS and the rest. Chinese customers
	// and older Outlook installations still send GB2312 constantly; without
	// this every such message becomes mojibake, which reads as our bug.
	"github.com/emersion/go-message/charset"
	gomail "github.com/emersion/go-message/mail"
)

// ParsedMail is one received message, reduced to what the application needs.
type ParsedMail struct {
	MessageID  string
	InReplyTo  string
	References []string
	FromEmail  string
	FromName   string
	// ToEmail 是 To 里的**第一个**地址；ToAll 是整段 To 头（解码后，原样）。
	// 一封群发给七个人的信，前者是「第一个收件人」，后者才是「发给了谁」。
	ToEmail string
	ToAll   string
	// 真正的回信地址。与 From 不同时，「点回复会发给谁」和「谁写的」就是两个
	// 答案 —— 商业邮件诈骗最常用的一手正是改这里。
	ReplyTo string
	// 抄送，原样保留（含显示名）：谁在这段对话里能看到，本身就是业务事实。
	CC string
	// 收信服务器验过的身份：SPF 通过的信封域、DKIM 签名的域。
	// 只在通过时有值，见 parseAuthResults。
	AuthSPF     string
	AuthDKIM    string
	Subject     string
	BodyHTML    string
	BodyText    string
	SentAt      time.Time
	Attachments []ParsedAttachment
	IsBounce    bool
	// For a bounce: which address failed and how permanently.
	BounceRecipient string
	BouncePermanent bool
	BounceDetail    string
}

type ParsedAttachment struct {
	FileName    string
	ContentType string
	Data        []byte
	// Content-ID, angle brackets stripped. Set only when the message gave the
	// part a name of its own to be pointed at by — which in practice means a
	// picture the body embeds, a signature logo above all.
	//
	// It was discarded until now, and that is why an embedded logo rendered as
	// a broken image: the bytes were stored, the body said cid:<this>, and
	// nothing joined the two. See migration 00031.
	ContentID string
}

// ParseMail turns raw RFC 5322 bytes into something storable.
//
// Deliberately forgiving. Real mail is full of messages that violate the
// specification — missing Content-Type, mislabelled charsets, headers that
// were never encoded — and refusing them means a customer's reply silently
// never appears. Anything unparseable degrades to "what we could read"
// rather than to an error.
func ParseMail(raw []byte) (ParsedMail, error) {
	var out ParsedMail

	ent, err := emsg.Read(bytes.NewReader(raw))
	if err != nil && ent == nil {
		return out, err
	}

	h := gomail.Header{Header: ent.Header}
	out.MessageID = trimAngles(firstHeader(ent, "Message-Id"))
	out.InReplyTo = trimAngles(firstHeader(ent, "In-Reply-To"))
	out.References = splitIDs(firstHeader(ent, "References"))
	out.Subject = decodeHeader(firstHeader(ent, "Subject"))

	if addrs, err := h.AddressList("From"); err == nil && len(addrs) > 0 {
		out.FromEmail = strings.ToLower(addrs[0].Address)
		out.FromName = addrs[0].Name
	} else {
		out.FromEmail, out.FromName = looseAddress(firstHeader(ent, "From"))
	}
	if addrs, err := h.AddressList("To"); err == nil && len(addrs) > 0 {
		out.ToEmail = strings.ToLower(addrs[0].Address)
	} else {
		out.ToEmail, _ = looseAddress(firstHeader(ent, "To"))
	}
	// 整段留着，和 Cc 一个存法。只留第一个的年代，客户群发给七个同事的信在
	// 这里变成了「发给一个人」，而剩下六个再也找不回来——除非回到原件。
	out.ToAll = decodeHeader(firstHeader(ent, "To"))
	if addrs, err := h.AddressList("Reply-To"); err == nil && len(addrs) > 0 {
		out.ReplyTo = strings.ToLower(addrs[0].Address)
	} else {
		out.ReplyTo, _ = looseAddress(firstHeader(ent, "Reply-To"))
	}
	out.CC = decodeHeader(firstHeader(ent, "Cc"))
	// 第一条，不是全部：这个头是经手的服务器写的，上游那些爱写什么写什么。
	// 最上面那条是我们自己的邮箱主机加的，也只有它可信。
	out.AuthSPF, out.AuthDKIM = parseAuthResults(firstHeader(ent, "Authentication-Results"))

	if t, err := h.Date(); err == nil {
		out.SentAt = t
	}

	ct, ctParams, _ := ent.Header.ContentType()
	out.IsBounce = looksLikeBounce(ct, ctParams, out.FromEmail, out.Subject)

	// One pass only. A MIME reader is a single-use stream: walking the tree
	// consumes it, so anything a second pass wanted to read is already gone.
	// The delivery-status part is therefore collected during the walk rather
	// than by going round again.
	walk(ent, &out, 0)
	return out, nil
}

// walk descends the MIME tree collecting bodies and attachments.
//
// Depth-limited: a crafted message can nest message/rfc822 parts thousands
// deep, and an unbounded recursion on untrusted input is a denial of service
// waiting to be discovered by a spammer rather than by us.
func walk(ent *emsg.Entity, out *ParsedMail, depth int) {
	const maxDepth = 20
	if depth > maxDepth {
		return
	}

	mr := ent.MultipartReader()
	if mr == nil {
		collectLeaf(ent, out)
		return
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return
		}
		if err != nil {
			// A malformed part should not cost us the parts we already read.
			return
		}
		walk(part, out, depth+1)
	}
}

func collectLeaf(ent *emsg.Entity, out *ParsedMail) {
	ct, _, _ := ent.Header.ContentType()
	disp, dispParams, _ := ent.Header.ContentDisposition()

	// A machine-readable delivery report. Its mere presence is a stronger
	// bounce signal than any header sniffing, so it settles the question as
	// well as supplying the verdict.
	if strings.HasPrefix(ct, "message/delivery-status") {
		out.IsBounce = true
		readDeliveryStatus(ent, out)
		return
	}

	filename := dispParams["filename"]
	if filename == "" {
		if _, p, err := ent.Header.ContentType(); err == nil {
			filename = p["name"]
		}
	}
	contentID := contentIDOf(ent.Header.Get("Content-ID"))

	// An attachment is anything explicitly marked as one, anything carrying a
	// filename (plenty of clients send Content-Disposition: inline for a real
	// attachment), and anything carrying a Content-ID.
	//
	// That last clause is not a nicety. An image pasted into Gmail's composer
	// arrives like this:
	//
	//	Content-Type: image/png
	//	Content-Disposition: inline          <- no filename= parameter
	//	Content-ID: <ii_19fe9d231d101>
	//
	// No filename anywhere, and the disposition is "inline" rather than
	// "attachment", so a filename-only rule walks straight past it and the
	// bytes are dropped. The body still says <img src="cid:ii_19fe9d231d101">,
	// so the reader renders an empty box — which is exactly what a customer
	// pasting a photo of a damaged carton produced. A part the body points at
	// by Content-ID is referenced content by definition; whether its sender
	// bothered to name it is beside the point.
	// Except when the part carrying the identifier is the body itself.
	// LinkedIn labels its two alternatives Content-ID: text-body and
	// html-body, and the clause above swallowed both: the message arrived
	// with no body at all and two files called attachment.img, which is
	// precisely what the reader showed.
	//
	// The clause is about parts the body *points at* by cid:, and a body
	// cannot point at itself. So a text alternative that nobody dispositioned
	// as an attachment and that carries no filename of its own is a body,
	// whatever identifier its sender chose to give it. An .html file someone
	// genuinely attached still has a filename or an explicit disposition, and
	// still lands below.
	selfNamedBody := contentID != "" && filename == "" && disp != "attachment" &&
		(strings.HasPrefix(ct, "text/html") || strings.HasPrefix(ct, "text/plain"))

	if disp == "attachment" || (contentID != "" && !selfNamedBody) ||
		(filename != "" && !strings.HasPrefix(ct, "text/")) {
		data, err := io.ReadAll(io.LimitReader(ent.Body, maxAttachmentBytes))
		if err != nil {
			return
		}
		out.Attachments = append(out.Attachments, ParsedAttachment{
			FileName:    attachmentName(decodeHeader(filename), ct, contentID),
			ContentType: ct,
			Data:        data,
			ContentID:   contentID,
		})
		return
	}

	body, err := io.ReadAll(io.LimitReader(ent.Body, maxBodyBytes))
	if err != nil {
		return
	}
	switch {
	case strings.HasPrefix(ct, "text/html"):
		// First one wins: a forwarded chain repeats the body at every level,
		// and appending would show the same text several times over.
		if out.BodyHTML == "" {
			out.BodyHTML = string(body)
		}
	case strings.HasPrefix(ct, "text/plain"):
		if out.BodyText == "" {
			out.BodyText = string(body)
		}
	}
}

// attachmentName is what the file is called when its sender did not say.
//
// A name is needed even for a part nobody will ever click: the download path
// saves under it, the repair pass in embedded.go matches on (name, size), and
// an empty string in a list column reads as a broken row. Derived from the
// content type rather than from the Content-ID, because the identifier is a
// random token — "ii_19fe9d231d101" tells a person nothing, "image.png" tells
// them what it is.
func attachmentName(filename, contentType, contentID string) string {
	if filename != "" {
		return filename
	}
	if contentID == "" {
		return "attachment"
	}
	base := "image"
	if !strings.HasPrefix(contentType, "image/") {
		base = "attachment"
	}
	return base + extensionFor(strings.ToLower(contentType))
}

const (
	// A single attachment beyond this is refused rather than held in memory.
	maxAttachmentBytes = 25 << 20
	maxBodyBytes       = 5 << 20
)

// looksLikeBounce recognises a delivery report before it is shown to anybody.
//
// A bounce arrives looking like ordinary mail. Left alone it would sit in a
// salesperson's inbox as a message from "Mail Delivery Subsystem" while the
// customer it concerns stays marked as successfully contacted — the failure
// mode this whole inbound path exists to close.
func looksLikeBounce(contentType string, params map[string]string, from, subject string) bool {
	if strings.HasPrefix(contentType, "multipart/report") {
		if rt := strings.ToLower(params["report-type"]); rt == "delivery-status" || rt == "" {
			return true
		}
	}
	local := from
	if i := strings.Index(local, "@"); i > 0 {
		local = local[:i]
	}
	switch strings.ToLower(local) {
	case "mailer-daemon", "postmaster":
		return true
	}
	s := strings.ToLower(subject)
	for _, hint := range []string{
		"undeliverable", "delivery status notification", "returned mail",
		"mail delivery failed", "delivery failure", "退信",
	} {
		if strings.Contains(s, hint) {
			return true
		}
	}
	return false
}

// readBounceReport digs the verdict out of the message/delivery-status part.
//
// The Status field is what decides whether an address is dead or merely busy:
// 5.x.x means stop writing to it, 4.x.x means the far side was temporarily
// unable. Suppressing on a 4.x.x would silently blacklist a customer whose
// mailbox was briefly full.
// contentIDOf normalises the header into the form a body writes.
//
// RFC 2392: the header is <angle-bracketed>, and the src that points at it is
// "cid:" followed by the same value without the brackets. Clients are sloppy
// about whitespace and a few omit the brackets entirely, so both are accepted
// and the bare value is what gets stored — that is the string the lookup will
// be handed.
func contentIDOf(raw string) string {
	v := strings.TrimSpace(raw)
	v = strings.TrimPrefix(v, "<")
	v = strings.TrimSuffix(v, ">")
	// A Content-ID is an addr-spec and cannot legally contain whitespace; one
	// that does is mangled beyond a guess, and a mangled id that half-matches
	// would attach the wrong picture.
	if v == "" || strings.ContainsAny(v, " \t\r\n") {
		return ""
	}
	return v
}

func readDeliveryStatus(ent *emsg.Entity, out *ParsedMail) {
	body, err := io.ReadAll(io.LimitReader(ent.Body, 64<<10))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "final-recipient:"), strings.HasPrefix(lower, "original-recipient:"):
			if out.BounceRecipient == "" {
				v := line[strings.Index(line, ":")+1:]
				if i := strings.Index(v, ";"); i >= 0 {
					v = v[i+1:]
				}
				out.BounceRecipient = strings.ToLower(strings.Trim(strings.TrimSpace(v), "<>"))
			}
		case strings.HasPrefix(lower, "status:"):
			code := strings.TrimSpace(line[len("status:"):])
			out.BounceDetail = code
			out.BouncePermanent = strings.HasPrefix(code, "5.")
		case strings.HasPrefix(lower, "diagnostic-code:"):
			if out.BounceDetail == "" {
				out.BounceDetail = strings.TrimSpace(line[len("diagnostic-code:"):])
			}
		}
	}
}

// OriginalMessageIDs pulls the Message-IDs of whatever a bounce is about.
//
// A delivery report carries the message it failed to deliver, either whole
// (message/rfc822) or as headers alone (text/rfc822-headers). Either way its
// Message-ID is ours, and that is what ties the bounce back to the row that
// says we sent it.
func OriginalMessageIDs(raw []byte) []string {
	ent, err := emsg.Read(bytes.NewReader(raw))
	if err != nil && ent == nil {
		return nil
	}
	var found []string
	var scan func(e *emsg.Entity, depth int)
	scan = func(e *emsg.Entity, depth int) {
		if depth > 10 {
			return
		}
		ct, _, _ := e.Header.ContentType()
		if strings.HasPrefix(ct, "message/rfc822") || strings.HasPrefix(ct, "text/rfc822-headers") {
			body, err := io.ReadAll(io.LimitReader(e.Body, 256<<10))
			if err == nil {
				// text/rfc822-headers has no body, so net/mail's reader is
				// the tolerant option here — it stops at the blank line and
				// does not mind that nothing follows it.
				if m, err := netmail.ReadMessage(bytes.NewReader(body)); err == nil {
					if id := m.Header.Get("Message-Id"); id != "" {
						found = append(found, trimAngles(id))
					}
				}
			}
			return
		}
		mr := e.MultipartReader()
		if mr == nil {
			return
		}
		for {
			p, err := mr.NextPart()
			if err != nil {
				return
			}
			scan(p, depth+1)
		}
	}
	scan(ent, 0)
	return found
}

func firstHeader(ent *emsg.Entity, key string) string {
	return ent.Header.Get(key)
}

// decodeHeader turns =?GB2312?B?...?= into readable text.
//
// The CharsetReader is the point: Go's own decoder handles UTF-8 and
// ISO-8859-1 and gives up on everything else, which covers roughly none of
// the mail a Chinese exporter receives.
func decodeHeader(s string) string {
	if s == "" {
		return ""
	}
	d := &mime.WordDecoder{CharsetReader: charset.Reader}
	if out, err := d.DecodeHeader(s); err == nil {
		return out
	}
	return s
}

func trimAngles(s string) string {
	return strings.Trim(strings.TrimSpace(s), "<>")
}

// splitIDs breaks a References header into individual ids. They are
// whitespace separated in theory and comma separated in the wild.
func splitIDs(s string) []string {
	f := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == ','
	})
	out := make([]string, 0, len(f))
	for _, v := range f {
		if v = trimAngles(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// looseAddress salvages an address from a header net/mail refused to parse.
// Unquoted display names containing commas are the usual culprit, and losing
// the sender of a real customer message over punctuation is not acceptable.
func looseAddress(s string) (email, name string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if i := strings.LastIndex(s, "<"); i >= 0 {
		if j := strings.Index(s[i:], ">"); j > 0 {
			email = strings.ToLower(strings.TrimSpace(s[i+1 : i+j]))
			name = decodeHeader(strings.TrimSpace(strings.Trim(s[:i], ` "`)))
			return email, name
		}
	}
	if strings.Contains(s, "@") {
		return strings.ToLower(s), ""
	}
	return "", decodeHeader(s)
}
