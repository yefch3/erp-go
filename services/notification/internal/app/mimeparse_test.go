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
