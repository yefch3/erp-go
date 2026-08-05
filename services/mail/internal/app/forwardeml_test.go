package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// Attaching a file to a send used to require its key to sit under the prefix
// PresignAttachment hands out. Received mail is not stored there — it lives
// under mail/inbound/<tenant>/<account>/ — so forwarding anything that had an
// attachment was refused outright with 文件标识无效, and forward-as-attachment
// would have been refused for the same reason.
//
// The fix could not be "also allow mail/inbound/", because that path is keyed
// by account: a caller who could name it could name a colleague's account and
// attach their mail's files. So the trusted path is marked in code the caller
// cannot reach. These tests pin both halves of that — what the prefix must
// still refuse, and what the forward path must be allowed.

func TestAttachmentKeyTrustBoundary(t *testing.T) {
	const tenant = 1

	cases := []struct {
		name  string
		att   PendingAttachment
		allow bool
		why   string
	}{
		{
			name:  "own upload",
			att:   PendingAttachment{FileKey: "mail-attachments/1/ccaaafba026d22a09fadf124909137d0.pdf"},
			allow: true,
			why:   "this is what PresignAttachment hands the browser",
		},
		{
			name:  "another tenant's upload",
			att:   PendingAttachment{FileKey: "mail-attachments/2/ccaaafba026d22a09fadf124909137d0.pdf"},
			allow: false,
			why:   "the prefix is per tenant precisely so this fails",
		},
		{
			name:  "inbound key pasted by the client",
			att:   PendingAttachment{FileKey: "mail/inbound/1/2/att/7695-contract.pdf"},
			allow: false,
			why: "the attack the prefix exists to stop: mail/inbound/ is keyed by " +
				"account, so accepting a client-named key here would let one " +
				"employee attach a colleague's mail",
		},
		{
			name:  "raw .eml pasted by the client",
			att:   PendingAttachment{FileKey: "mail/inbound/1/2/16812.eml"},
			allow: false,
			why:   "same, and a whole message rather than one of its files",
		},
		{
			name:  "forwarded file, resolved by the server",
			att:   fromInbox("contract.pdf", "mail/inbound/1/2/att/7695-contract.pdf"),
			allow: true,
			why:   "read off a row whose owner_id was checked first",
		},
		{
			name:  "forwarded original, resolved by the server",
			att:   fromInbox("Re Q3.eml", "mail/inbound/1/2/16812.eml"),
			allow: true,
			why:   "forward-as-attachment: the message itself",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.att.allowedFor(tenant); got != c.allow {
				t.Errorf("allowedFor(%d) = %v, want %v\nkey: %s\nwhy: %s",
					tenant, got, c.allow, c.att.FileKey, c.why)
			}
		})
	}
}

// The whole guarantee above rests on a caller being unable to claim a key is
// server-derived. That is enforced by the field being unexported, which no
// test of behaviour would notice if somebody exported it "for convenience" —
// every case above would still pass while the boundary was gone.
func TestClientCannotClaimAKeyIsServerDerived(t *testing.T) {
	body := `{"fileName":"x.pdf",
	          "fileKey":"mail/inbound/1/2/att/7695-contract.pdf",
	          "serverDerived":true,"ServerDerived":true,"trusted":true}`

	var att PendingAttachment
	if err := json.Unmarshal([]byte(body), &att); err != nil {
		t.Fatalf("decoding the request body failed: %v", err)
	}
	if att.allowedFor(1) {
		t.Fatal("a request body talked its way past the attachment guard;\n" +
			"serverDerived must stay unexported so only this package can set it")
	}
}

func TestForwardedOriginalIsNamedAfterItsSubject(t *testing.T) {
	cases := []struct {
		subject string
		want    string
	}{
		{"Q3 report", "Q3_report.eml"},
		{"  Re: 报价单  ", "Re:_报价单.eml"},
		// A path separator in a subject must not become one in a filename.
		{"invoices/2026", "invoices_2026.eml"},
		// Both ".." and "/" are replaced, so a traversal attempt survives only
		// as underscores. Nothing here is ever used as a path — this is the
		// name the recipient's client shows — but a subject is attacker-
		// supplied text and should not arrive looking like a path at all.
		{"../../etc/passwd", "____etc_passwd.eml"},
		// Never a bare extension: a file called ".eml" reads as broken.
		{"", "forwarded-message.eml"},
		{"   ", "forwarded-message.eml"},
	}

	for _, c := range cases {
		if got := emlFileName(c.subject); got != c.want {
			t.Errorf("emlFileName(%q) = %q, want %q", c.subject, got, c.want)
		}
	}

	long := emlFileName(strings.Repeat("题", 200))
	if !strings.HasSuffix(long, ".eml") {
		t.Errorf("a long subject lost its extension: %q", long)
	}
}
