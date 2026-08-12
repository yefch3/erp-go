package app

import (
	"strings"
	"testing"
)

// A body part is allowed to name itself.
//
// "Anything with a Content-ID is an attachment" was added so that a photo
// pasted into Gmail's composer - inline, no filename, identified only by
// Content-ID - would be stored and could be joined back to the <img src="cid:">
// that referenced it. The rule is about parts the body points at.
//
// LinkedIn puts a Content-ID on the body itself: its multipart/alternative
// carries Content-ID: text-body and Content-ID: html-body. The rule swallowed
// both, and the message arrived with no body at all and two files called
// attachment.img. Ten messages in one mailbox were in that state, every one
// of them from LinkedIn, which is a large enough sender that "our reader shows
// an empty message" would have been reported by the pilot in its first week.

// linkedInShapedMail mirrors the structure of a real LinkedIn notification:
// multipart/alternative, both alternatives labelled with a Content-ID, no
// filename and no Content-Disposition on either.
const linkedInShapedMail = "From: Someone <inmail@example.com>\r\n" +
	"To: Recipient <recipient@example.com>\r\n" +
	"Subject: A role you might like\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/alternative; \r\n" +
	"\tboundary=\"----=_Part_1_2.3\"\r\n" +
	"\r\n" +
	"------=_Part_1_2.3\r\n" +
	"Content-Type: text/plain;charset=UTF-8\r\n" +
	"Content-ID: text-body\r\n" +
	"\r\n" +
	"Hi, I am reaching out about an opening.\r\n" +
	"------=_Part_1_2.3\r\n" +
	"Content-Type: text/html;charset=UTF-8\r\n" +
	"Content-ID: html-body\r\n" +
	"\r\n" +
	"<html><body><p>Hi, I am reaching out about an opening.</p></body></html>\r\n" +
	"------=_Part_1_2.3--\r\n"

func TestBodyPartWithContentIDIsBodyNotAttachment(t *testing.T) {
	got, err := ParseMail([]byte(linkedInShapedMail))
	if err != nil {
		t.Fatalf("ParseMail: %v", err)
	}

	if !strings.Contains(got.BodyHTML, "reaching out about an opening") {
		t.Errorf("BodyHTML = %q, want the html alternative;\n"+
			"a Content-ID on the body does not make the body an attachment - "+
			"the reader shows an empty message when it does", got.BodyHTML)
	}
	if !strings.Contains(got.BodyText, "reaching out about an opening") {
		t.Errorf("BodyText = %q, want the plain alternative", got.BodyText)
	}
	if len(got.Attachments) != 0 {
		names := make([]string, 0, len(got.Attachments))
		for _, a := range got.Attachments {
			names = append(names, a.FileName+" ("+a.ContentType+")")
		}
		t.Errorf("got %d attachments %v, want none: those are the body",
			len(got.Attachments), names)
	}
}

// The rule this narrows must keep doing its job: an image pasted into a
// composer arrives inline, unnamed, and identified only by Content-ID, and
// dropping it is what made a customer's photo render as an empty box.
const pastedImageMail = "From: Customer <customer@example.com>\r\n" +
	"To: Sales <sales@example.com>\r\n" +
	"Subject: Damaged carton\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/related; boundary=\"b1\"\r\n" +
	"\r\n" +
	"--b1\r\n" +
	"Content-Type: text/html;charset=UTF-8\r\n" +
	"\r\n" +
	"<html><body>See <img src=\"cid:ii_19fe9d231d101\"></body></html>\r\n" +
	"--b1\r\n" +
	"Content-Type: image/png\r\n" +
	"Content-Disposition: inline\r\n" +
	"Content-ID: <ii_19fe9d231d101>\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"\r\n" +
	"iVBORw0KGgo=\r\n" +
	"--b1--\r\n"

func TestPastedImageIsStillAnAttachment(t *testing.T) {
	got, err := ParseMail([]byte(pastedImageMail))
	if err != nil {
		t.Fatalf("ParseMail: %v", err)
	}
	if len(got.Attachments) != 1 {
		t.Fatalf("got %d attachments, want the pasted image", len(got.Attachments))
	}
	if got.Attachments[0].ContentID != "ii_19fe9d231d101" {
		t.Errorf("ContentID = %q, want it kept so the cid: reference can be joined back",
			got.Attachments[0].ContentID)
	}
	if !strings.Contains(got.BodyHTML, "cid:ii_19fe9d231d101") {
		t.Errorf("BodyHTML = %q, want the html part that points at the image", got.BodyHTML)
	}
}

// An .html file somebody genuinely attached is still an attachment, even
// though it is text/*: it has a filename, which is what separates "a file
// that happens to be html" from "the message".
const attachedHTMLFileMail = "From: A <a@example.com>\r\n" +
	"To: B <b@example.com>\r\n" +
	"Subject: Report\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=\"b2\"\r\n" +
	"\r\n" +
	"--b2\r\n" +
	"Content-Type: text/plain;charset=UTF-8\r\n" +
	"\r\n" +
	"Report attached.\r\n" +
	"--b2\r\n" +
	"Content-Type: text/html;charset=UTF-8;name=\"report.html\"\r\n" +
	"Content-Disposition: attachment; filename=\"report.html\"\r\n" +
	"Content-ID: report-file\r\n" +
	"\r\n" +
	"<html><body>Q3</body></html>\r\n" +
	"--b2--\r\n"

func TestAttachedHTMLFileIsStillAnAttachment(t *testing.T) {
	got, err := ParseMail([]byte(attachedHTMLFileMail))
	if err != nil {
		t.Fatalf("ParseMail: %v", err)
	}
	if len(got.Attachments) != 1 {
		t.Fatalf("got %d attachments, want report.html", len(got.Attachments))
	}
	if got.Attachments[0].FileName != "report.html" {
		t.Errorf("FileName = %q, want report.html", got.Attachments[0].FileName)
	}
	if !strings.Contains(got.BodyText, "Report attached") {
		t.Errorf("BodyText = %q, want the plain part", got.BodyText)
	}
	if strings.Contains(got.BodyHTML, "Q3") {
		t.Error("the attached file was used as the message body")
	}
}
