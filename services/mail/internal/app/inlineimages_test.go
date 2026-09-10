package app

import (
	"strings"
	"testing"
)

// The distinct tokens the body references, in first-appearance order. One logo
// used twice is one part carried once with two references to it — carrying it
// twice would double the size of every mail with a repeated image.
func TestATokenUsedTwiceIsCarriedOnce(t *testing.T) {
	body := `<img src="https://erp.example.com/api/public/mail-images/aaa">` +
		`<img src="https://erp.example.com/api/public/mail-images/bbb">` +
		`<img src="https://erp.example.com/api/public/mail-images/aaa">`
	got := mailImageTokens(body)
	if len(got) != 2 || got[0] != "aaa" || got[1] != "bbb" {
		t.Fatalf("got %v, want [aaa bbb]", got)
	}
}

// The tracking pixel is on a different route and must never be inlined: it
// works precisely because the recipient has to come and fetch it.
func TestTheTrackingPixelIsNotAnInlineCandidate(t *testing.T) {
	body := `<img src="https://erp.example.com/api/public/mail-open/abc123" width="1">`
	if got := mailImageTokens(body); len(got) != 0 {
		t.Fatalf("the pixel was picked up as an image: %v", got)
	}
}

// Somebody else's picture stays somebody else's.
func TestOtherHostsAreNotInlineCandidates(t *testing.T) {
	body := `<img src="https://customer.example.com/logo.png">`
	if got := mailImageTokens(body); len(got) != 0 {
		t.Fatalf("an unrelated image was picked up: %v", got)
	}
}

// A service with no storage configured must not rewrite anything: a cid:
// pointing at a part that is not in the message is worse than the link it
// replaced, because the link at least has a chance of loading.
func TestWithNoStorageNothingIsInlined(t *testing.T) {
	s := &Service{}
	body := `<img src="https://erp.example.com/api/public/mail-images/aaa">`
	got, imgs := s.InlineMailImages(t.Context(), 1, 2, body)
	if got != body || len(imgs) != 0 {
		t.Fatalf("rewrote without being able to carry anything: %q / %v", got, imgs)
	}
	if strings.Contains(got, "cid:") {
		t.Error("a dangling cid: was written")
	}
}
