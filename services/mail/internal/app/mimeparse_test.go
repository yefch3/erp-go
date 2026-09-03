package app

import (
	"strings"
	"testing"
	"time"
)

func TestParsePlainMessage(t *testing.T) {
	raw := "From: Klaus Weber <klaus@example.com>\r\n" +
		"To: lina@sunrise.com\r\n" +
		"Subject: Re: Q3 offer\r\n" +
		"Message-ID: <abc123@example.com>\r\n" +
		"In-Reply-To: <key-1@sunrise.com>\r\n" +
		"Date: Fri, 31 Jul 2026 10:00:00 +0000\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"Thanks, the price works for us.\r\n"

	got, err := ParseMail([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got.FromEmail != "klaus@example.com" || got.FromName != "Klaus Weber" {
		t.Fatalf("From wrong: %q / %q", got.FromEmail, got.FromName)
	}
	if got.Subject != "Re: Q3 offer" {
		t.Fatalf("Subject wrong: %q", got.Subject)
	}
	if got.MessageID != "abc123@example.com" {
		t.Fatalf("Message-ID should have its angle brackets stripped: %q", got.MessageID)
	}
	if got.InReplyTo != "key-1@sunrise.com" {
		t.Fatalf("In-Reply-To wrong: %q", got.InReplyTo)
	}
	if !strings.Contains(got.BodyText, "price works") {
		t.Fatalf("body lost: %q", got.BodyText)
	}
	if got.IsBounce {
		t.Fatal("an ordinary reply was classified as a bounce")
	}
}

// GB2312 is still everywhere in Chinese business mail. Without charset
// decoding every such message becomes mojibake, which reads as our bug.
func TestGb2312SubjectDecodes(t *testing.T) {
	raw := "From: =?GB2312?B?wfXTwA==?= <liu@example.cn>\r\n" +
		"Subject: =?GB2312?B?serXvL/Vus/NqLn6uPHIyw==?=\r\n" +
		"Message-ID: <cn1@example.cn>\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"hello\r\n"

	got, err := ParseMail([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	// The exact characters matter less than the guarantee that the encoded
	// word was decoded at all rather than passed through raw.
	if strings.Contains(got.Subject, "=?GB2312?") {
		t.Fatalf("GB2312 subject was not decoded: %q", got.Subject)
	}
	if strings.Contains(got.FromName, "=?GB2312?") {
		t.Fatalf("GB2312 display name was not decoded: %q", got.FromName)
	}
}

func TestMultipartAlternativePrefersBothParts(t *testing.T) {
	raw := "From: a@example.com\r\nTo: b@example.com\r\nSubject: s\r\n" +
		"Message-ID: <m1@example.com>\r\n" +
		"Content-Type: multipart/alternative; boundary=BB\r\n\r\n" +
		"--BB\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nplain version\r\n" +
		"--BB\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>html version</p>\r\n" +
		"--BB--\r\n"

	got, _ := ParseMail([]byte(raw))
	if !strings.Contains(got.BodyText, "plain version") {
		t.Fatalf("text part lost: %q", got.BodyText)
	}
	if !strings.Contains(got.BodyHTML, "html version") {
		t.Fatalf("html part lost: %q", got.BodyHTML)
	}
}

func TestAttachmentIsExtracted(t *testing.T) {
	raw := "From: a@example.com\r\nSubject: s\r\nMessage-ID: <m2@example.com>\r\n" +
		"Content-Type: multipart/mixed; boundary=CC\r\n\r\n" +
		"--CC\r\nContent-Type: text/plain\r\n\r\nsee attached\r\n" +
		"--CC\r\nContent-Type: application/pdf\r\n" +
		"Content-Disposition: attachment; filename=\"offer.pdf\"\r\n\r\n" +
		"%PDF-fake\r\n" +
		"--CC--\r\n"

	got, _ := ParseMail([]byte(raw))
	if len(got.Attachments) != 1 {
		t.Fatalf("want 1 attachment, got %d", len(got.Attachments))
	}
	a := got.Attachments[0]
	if a.FileName != "offer.pdf" || !strings.Contains(string(a.Data), "%PDF-fake") {
		t.Fatalf("attachment wrong: %+v", a)
	}
	if !strings.Contains(got.BodyText, "see attached") {
		t.Fatal("the body was lost when an attachment was present")
	}
}

// A bounce that reaches the inbox as ordinary mail is the failure this whole
// inbound path exists to close: the customer looks contacted and is not.
func TestDeliveryReportIsRecognisedAndRead(t *testing.T) {
	raw := "From: Mail Delivery Subsystem <mailer-daemon@googlemail.com>\r\n" +
		"To: lina@sunrise.com\r\n" +
		"Subject: Delivery Status Notification (Failure)\r\n" +
		"Message-ID: <bounce1@googlemail.com>\r\n" +
		"Content-Type: multipart/report; report-type=delivery-status; boundary=DD\r\n\r\n" +
		"--DD\r\nContent-Type: text/plain\r\n\r\nYour message was not delivered.\r\n" +
		"--DD\r\nContent-Type: message/delivery-status\r\n\r\n" +
		"Final-Recipient: rfc822; nosuch@example.com\r\n" +
		"Action: failed\r\n" +
		"Status: 5.1.1\r\n" +
		"Diagnostic-Code: smtp; 550 5.1.1 No such user\r\n" +
		"--DD\r\nContent-Type: message/rfc822\r\n\r\n" +
		"Message-ID: <key-42@sunrise.com>\r\nSubject: Q3 offer\r\n\r\nbody\r\n" +
		"--DD--\r\n"

	got, err := ParseMail([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsBounce {
		t.Fatal("a multipart/report delivery-status was not recognised as a bounce")
	}
	if got.BounceRecipient != "nosuch@example.com" {
		t.Fatalf("bounce recipient wrong: %q", got.BounceRecipient)
	}
	if !got.BouncePermanent {
		t.Fatal("5.1.1 must be permanent — retrying it for ever helps nobody")
	}

	ids := OriginalMessageIDs([]byte(raw))
	if len(ids) != 1 || ids[0] != "key-42@sunrise.com" {
		t.Fatalf("could not recover the original Message-ID: %v", ids)
	}
}

// 4.x.x is the far side being briefly unable, not an address that is wrong.
// Suppressing on one would quietly end a real correspondence.
func TestTransientBounceIsNotPermanent(t *testing.T) {
	raw := "From: postmaster@example.com\r\nSubject: Delayed Mail\r\n" +
		"Content-Type: multipart/report; report-type=delivery-status; boundary=EE\r\n\r\n" +
		"--EE\r\nContent-Type: message/delivery-status\r\n\r\n" +
		"Final-Recipient: rfc822; busy@example.com\r\nStatus: 4.2.2\r\n" +
		"--EE--\r\n"

	got, _ := ParseMail([]byte(raw))
	if !got.IsBounce {
		t.Fatal("not recognised as a delivery report")
	}
	if got.BouncePermanent {
		t.Fatal("4.2.2 was treated as permanent; that suppresses a working address")
	}
}

func TestMessageKeyExtraction(t *testing.T) {
	cases := map[string]string{
		"<key-42@sunrise.com>": "key-42",
		"key-42@sunrise.com":   "key-42",
		"nokeyhere":            "",
		"@sunrise.com":         "",
		"":                     "",
	}
	for in, want := range cases {
		if got := messageKeyFromID(in); got != want {
			t.Errorf("messageKeyFromID(%q) = %q, want %q", in, got, want)
		}
	}
}

// net/mail rejects an unquoted display name containing a comma, and losing a
// real customer's message over punctuation is not acceptable.
func TestUnparseableFromStillYieldsAnAddress(t *testing.T) {
	raw := "From: Weber, Klaus <klaus@example.com>\r\nSubject: s\r\n" +
		"Message-ID: <m3@example.com>\r\n\r\nbody\r\n"

	got, _ := ParseMail([]byte(raw))
	if got.FromEmail != "klaus@example.com" {
		t.Fatalf("address lost on a malformed From: %q", got.FromEmail)
	}
}

// A crafted message can nest parts thousands deep; unbounded recursion on
// untrusted input is a denial of service waiting to be found by a spammer.
func TestDeeplyNestedMessageTerminates(t *testing.T) {
	var b strings.Builder
	b.WriteString("From: a@example.com\r\nSubject: s\r\nMessage-ID: <m4@x>\r\n")
	depth := 60
	for i := 0; i < depth; i++ {
		b.WriteString("Content-Type: multipart/mixed; boundary=B" + string(rune('a'+i%26)) + "\r\n\r\n")
		b.WriteString("--B" + string(rune('a'+i%26)) + "\r\n")
	}
	b.WriteString("Content-Type: text/plain\r\n\r\ndeep\r\n")

	done := make(chan struct{})
	go func() {
		_, _ = ParseMail([]byte(b.String()))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("parsing a deeply nested message did not terminate")
	}
}

// An image pasted into Gmail's composer, byte for byte as Gmail sends it.
//
// It carries no filename at all — not in Content-Disposition, not as a
// Content-Type name parameter — and its disposition is "inline", not
// "attachment". A rule that looks only for a filename walks straight past it,
// drops the bytes, and leaves the body pointing at a cid: that resolves to
// nothing. On screen that is an empty bordered box where the picture was.
//
// Taken from a real message in the mailbox (inbound 6919): multipart/related
// wrapping a multipart/alternative, with the image alongside.
func TestAPastedGmailImageIsKept(t *testing.T) {
	// A 1x1 PNG, base64, so the part is a genuine image.
	const onePixel = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

	raw := "From: Fangchen <fy2272@columbia.edu>\r\n" +
		"To: lina@sunrise.com\r\n" +
		"Subject: test\r\n" +
		"Message-ID: <gmail-inline@mail.gmail.com>\r\n" +
		"Date: Mon, 10 Aug 2026 03:58:24 +0000\r\n" +
		"Content-Type: multipart/related; boundary=\"outer\"\r\n\r\n" +
		"--outer\r\n" +
		"Content-Type: multipart/alternative; boundary=\"inner\"\r\n\r\n" +
		"--inner\r\n" +
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n\r\n" +
		"test\r\n" +
		"--inner\r\n" +
		"Content-Type: text/html; charset=\"UTF-8\"\r\n\r\n" +
		"<div>test<img src=\"cid:ii_19fe9d231d101\" width=\"95\" height=\"96\"></div>\r\n" +
		"--inner--\r\n" +
		"--outer\r\n" +
		"Content-Type: image/png\r\n" +
		"Content-Disposition: inline\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"Content-ID: <ii_19fe9d231d101>\r\n\r\n" +
		onePixel + "\r\n" +
		"--outer--\r\n"

	got, err := ParseMail([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Attachments) != 1 {
		t.Fatalf("the pasted image was dropped: %d attachments kept", len(got.Attachments))
	}
	a := got.Attachments[0]
	if a.ContentID != "ii_19fe9d231d101" {
		t.Errorf("ContentID is %q, so the body's cid: will never find it", a.ContentID)
	}
	if len(a.Data) == 0 {
		t.Error("the part was recognised but its bytes were not read")
	}
	if a.ContentType != "image/png" {
		t.Errorf("ContentType is %q", a.ContentType)
	}
	// A name is needed even though the sender gave none: the download path
	// saves under it and the repair pass matches on it.
	if a.FileName != "image.png" {
		t.Errorf("FileName is %q, want a derived one", a.FileName)
	}
	// The body must still be the HTML one, not the image.
	if !strings.Contains(got.BodyHTML, "cid:ii_19fe9d231d101") {
		t.Errorf("the body lost its reference: %q", got.BodyHTML)
	}
}

// A part with neither filename nor Content-ID is still not an attachment —
// widening the rule must not start collecting body parts as files.
func TestAPlainBodyPartIsStillNotAnAttachment(t *testing.T) {
	raw := "From: a@example.com\r\nTo: b@example.com\r\nSubject: s\r\n" +
		"Date: Mon, 10 Aug 2026 03:58:24 +0000\r\n" +
		"Content-Type: multipart/alternative; boundary=\"b\"\r\n\r\n" +
		"--b\r\nContent-Type: text/plain\r\n\r\nhello\r\n" +
		"--b\r\nContent-Type: text/html\r\n\r\n<p>hello</p>\r\n" +
		"--b--\r\n"
	got, err := ParseMail([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Attachments) != 0 {
		t.Fatalf("a body part was collected as an attachment: %+v", got.Attachments)
	}
}

// 客户群发给公司七个人：第一个是 ToEmail，七个都在 ToAll 里。
// 从前只留第一个，其余六个在库里根本不存在——这条钉的就是那个洞。
func TestToKeepsEveryRecipientNotJustTheFirst(t *testing.T) {
	raw := "From: MARILIN =?utf-8?q?LUDE=C3=91A?= <importaciones@acerosinka.com>\r\n" +
		"To: Ana Maria Gomez <agomez@acerosinka.com>, jgomez@acerosinka.com,\r\n" +
		" allosa@acerosinka.com, administracion@acerosinka.com\r\n" +
		"Cc: logistica@acerosinka.com\r\n" +
		"Subject: NEW RFQ\r\n" +
		"Message-ID: <rfq@acerosinka.com>\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"hola\r\n"
	got, err := ParseMail([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got.ToEmail != "agomez@acerosinka.com" {
		t.Fatalf("第一个收件人：%q", got.ToEmail)
	}
	for _, want := range []string{"agomez@", "jgomez@", "allosa@", "administracion@"} {
		if !strings.Contains(got.ToAll, want) {
			t.Fatalf("ToAll 少了 %s：%q", want, got.ToAll)
		}
	}
	if strings.Contains(got.ToAll, "logistica@") {
		t.Fatalf("Cc 不该混进 To：%q", got.ToAll)
	}
	if got.FromName != "MARILIN LUDEÑA" {
		t.Fatalf("发信人显示名没解码：%q", got.FromName)
	}
	if len(parseParties(got.ToAll)) != 4 {
		t.Fatalf("四个收件人应该拆出四个人：%+v", parseParties(got.ToAll))
	}
}

// QQ 邮箱（以及不少群发平台）把编码过的显示名再套一层引号发出来：
//
//	From: "=?utf-8?B?RnVuY3Rpb24gWWU=?=" <875172387@qq.com>
//
// RFC 2047 说引号里不该有编码词，net/mail 于是对引号里的内容原样保留，
// 列表和详情上的发件人就是那串 =?utf-8?B?…?=。
func TestQuotedEncodedWordInFromNameIsStillDecoded(t *testing.T) {
	for _, from := range []string{
		`"=?utf-8?B?RnVuY3Rpb24gWWU=?=" <875172387@qq.com>`, // QQ 的写法：套着引号
		`=?utf-8?B?RnVuY3Rpb24gWWU=?= <875172387@qq.com>`,   // 标准写法，本来就好
	} {
		p, err := ParseMail([]byte("From: " + from + "\r\nTo: erptest@263.net\r\n" +
			"Subject: x\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nhi\r\n"))
		if err != nil {
			t.Fatal(err)
		}
		if p.FromName != "Function Ye" || p.FromEmail != "875172387@qq.com" {
			t.Errorf("From %q: got %q <%s>, want Function Ye <875172387@qq.com>", from, p.FromName, p.FromEmail)
		}
	}
}
