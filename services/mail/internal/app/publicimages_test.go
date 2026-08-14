package app

import (
	"strings"
	"testing"
)

// The bug: the composer builds image links from window.location.origin, so a
// signature logo left the building pointing at http://localhost:5173. To every
// recipient "localhost" is their own machine, so the picture was a broken icon
// for all of them — while looking perfect to the sender, for whom that address
// genuinely resolves.
func TestOurOwnImageLinksLeaveWithThePublicAddress(t *testing.T) {
	const base = "https://erp.aaaindustryinc.com"
	cases := map[string]string{
		"a dev server origin":     `<img src="http://localhost:5173/api/public/mail-images/abc123" alt="">`,
		"the gateway's port":      `<img src="http://localhost:8080/api/public/mail-images/abc123" alt="">`,
		"whatever host was typed": `<img src="https://192.168.1.40:8080/api/public/mail-images/abc123">`,
		"already relative":        `<img src="/api/public/mail-images/abc123">`,
	}
	for name, body := range cases {
		got := AbsolutiseMailImages(body, base)
		want := base + "/api/public/mail-images/abc123"
		if !strings.Contains(got, want) {
			t.Errorf("%s: got %q, want it to contain %q", name, got, want)
		}
		if strings.Contains(got, "localhost") || strings.Contains(got, "192.168") {
			t.Errorf("%s: an unreachable host survived: %q", name, got)
		}
	}
}

// Somebody else's picture is somebody else's business. Rewriting a customer's
// own logo onto our host would break a link that worked.
func TestOtherPeoplesImagesAreLeftAlone(t *testing.T) {
	body := `<img src="https://customer.example.com/logo.png">` +
		`<img src="https://cdn.example.com/api/public/other/abc">`
	got := AbsolutiseMailImages(body, "https://erp.aaaindustryinc.com")
	if got != body {
		t.Fatalf("an unrelated image was rewritten:\n got %q\nwant %q", got, body)
	}
}

// No public address configured: leave the body as it is. A wrong link is worse
// than the sender's own, which at least works while they test locally. The
// open pixel is silenced by the same condition.
func TestWithNoPublicAddressNothingIsRewritten(t *testing.T) {
	body := `<img src="http://localhost:5173/api/public/mail-images/abc123">`
	if got := AbsolutiseMailImages(body, ""); got != body {
		t.Fatalf("got %q, want it untouched", got)
	}
	if got := AbsolutiseMailImages(body, "   "); got != body {
		t.Fatalf("whitespace counted as an address: %q", got)
	}
}

// A trailing slash on the configured address must not produce a double one:
// some hosts serve //api as a different path, and it looks careless besides.
func TestATrailingSlashDoesNotDoubleUp(t *testing.T) {
	got := AbsolutiseMailImages(
		`<img src="/api/public/mail-images/abc123">`, "https://erp.example.com/")
	if strings.Contains(got, "com//api") {
		t.Fatalf("doubled slash: %q", got)
	}
	if !strings.Contains(got, "https://erp.example.com/api/public/mail-images/abc123") {
		t.Fatalf("got %q", got)
	}
}

// Several pictures in one signature, and the same one twice, both have to be
// rewritten — a partial fix is still a broken mail.
func TestEveryOccurrenceIsRewritten(t *testing.T) {
	body := `<img src="http://localhost:5173/api/public/mail-images/aaa">` +
		`<img src="http://localhost:5173/api/public/mail-images/bbb">` +
		`<img src="http://localhost:5173/api/public/mail-images/aaa">`
	got := AbsolutiseMailImages(body, "https://erp.example.com")
	if strings.Contains(got, "localhost") {
		t.Fatalf("a link was left behind: %q", got)
	}
	if n := strings.Count(got, "https://erp.example.com/api/public/mail-images/"); n != 3 {
		t.Fatalf("rewrote %d of 3", n)
	}
}
