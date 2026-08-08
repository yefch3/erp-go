package app

import (
	"context"
	"strings"
	"testing"
)

// ------------------------------------------------------ reading the header

// The header is <angle-bracketed> and the body writes cid: plus the bare
// value. Storing one form and looking up the other is the whole bug this
// change exists to fix, so the normalisation is worth pinning down.
func TestTheIdentifierIsStoredInTheFormTheBodyWrites(t *testing.T) {
	for raw, want := range map[string]string{
		"<8E971712-2A98-45B3-B6A2-3F14F66CD9AD>": "8E971712-2A98-45B3-B6A2-3F14F66CD9AD",
		"  <logo@sender.example>  ":              "logo@sender.example",
		"bare-without-brackets@x":                "bare-without-brackets@x",
		"<part1.0A0B.0C0D@mail.gmail.com>":       "part1.0A0B.0C0D@mail.gmail.com",
		"":                                       "",
		"<>":                                     "",
		"<two words@x>":                          "", // mangled beyond a guess
		"<line\nbreak@x>":                        "",
	} {
		if got := contentIDOf(raw); got != want {
			t.Errorf("contentIDOf(%q) = %q, want %q", raw, got, want)
		}
	}
}

// A half-matching identifier would attach the wrong picture to a body, which
// is worse than leaving it broken — so anything ambiguous becomes empty and
// the part is treated as an ordinary attachment.
func TestAMangledIdentifierIsRefusedRatherThanGuessed(t *testing.T) {
	if got := contentIDOf("<image 001.png@sender>"); got != "" {
		t.Fatalf("a whitespace-bearing id was accepted as %q", got)
	}
}

// ------------------------------------------------------ finding the pointers

func TestEveryPointerInTheBodyIsFound(t *testing.T) {
	body := `<p>Regards,</p>
	<img src="cid:8E971712-2A98-45B3-B6A2-3F14F66CD9AD" alt="image.png">
	<img src='cid:logo@sender.example'>
	<img src="https://cdn.example/real.png">`
	got := bodyCIDs(body)
	for _, want := range []string{"8E971712-2A98-45B3-B6A2-3F14F66CD9AD", "logo@sender.example"} {
		if !got[want] {
			t.Errorf("missed %s; found %v", want, got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("a remote address was counted as an embedded one: %v", got)
	}
}

// ----------------------------------------------------- the substitution

// Both kinds of picture go through one substitution: remote ones keyed by
// their original address, embedded ones by cid:<identifier>.
func TestEmbeddedAndRemotePicturesAreSwappedInOnePass(t *testing.T) {
	body := `<img src="cid:logo@sender"><img src="https://cdn.example/banner.png">`
	swap := mergeSwaps(
		imageSwap{"https://cdn.example/banner.png": "https://storage/signed-banner"},
		imageSwap{"cid:logo@sender": "https://storage/signed-logo"},
	)
	out := (&Service{}).localiseImages(context.Background(), body, swap)
	for _, want := range []string{"https://storage/signed-logo", "https://storage/signed-banner"} {
		if !strings.Contains(out, want) {
			t.Errorf("%s is not in the result:\n%s", want, out)
		}
	}
	if strings.Contains(out, "cid:") {
		t.Fatalf("a cid: survived the swap:\n%s", out)
	}
}

// An identifier the message never carried stays as it was. Better a broken
// picture than a signed URL pointing at somebody else's file.
func TestAnUnknownIdentifierIsLeftAlone(t *testing.T) {
	body := `<img src="cid:never-stored@x">`
	out := (&Service{}).localiseImages(context.Background(), body,
		imageSwap{"cid:something-else@x": "https://storage/signed"})
	if !strings.Contains(out, "cid:never-stored@x") {
		t.Fatalf("an unknown identifier was rewritten:\n%s", out)
	}
}

func TestMergingSwapsKeepsBothSides(t *testing.T) {
	a := imageSwap{"one": "1"}
	b := imageSwap{"two": "2"}
	got := mergeSwaps(a, b)
	if len(got) != 2 || got["one"] != "1" || got["two"] != "2" {
		t.Fatalf("merge lost something: %v", got)
	}
	// The inputs must not be mutated: swapForThread hands out one map per
	// message and reuses the signatures behind them.
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("merge wrote into its arguments: a=%v b=%v", a, b)
	}
	if got := mergeSwaps(nil, b); len(got) != 1 {
		t.Fatalf("merging into nil lost the other side: %v", got)
	}
}

// ------------------------------------------------------ the attachment list

// A signature logo listed beside the signed contract is noise, and it is the
// second symptom of the same missing identifier.
func TestAPictureTheBodyAlreadyShowedIsNotListedAsAnAttachment(t *testing.T) {
	body := `<p>Per your request, the paystubs.</p>
	<img src="cid:sig-logo@unity" alt="image.png">`
	atts := []Attachment{
		{ID: 1, FileName: "image.png", ContentID: "sig-logo@unity"},
		{ID: 2, FileName: "17728-2026 paystubs.pdf"},
	}
	got := hideEmbedded(atts, body, imageSwap{"cid:sig-logo@unity": "https://storage/signed"})
	if len(got) != 1 || got[0].FileName != "17728-2026 paystubs.pdf" {
		t.Fatalf("the list is %v; only the real attachment should remain", names(got))
	}
}

// The test is "does the body point at it", not "did the sender mark it
// inline". Clients send Content-Disposition: inline for real attachments all
// the time, and hiding on that flag would make a customer's contract vanish.
func TestAPartNobodyPointsAtStaysInTheList(t *testing.T) {
	body := `<p>See attached.</p><img src="cid:shown@x">`
	atts := []Attachment{
		{ID: 1, FileName: "shown.png", ContentID: "shown@x"},
		// Carries an identifier but the body never mentions it — an inline
		// part the sender attached and then did not embed.
		{ID: 2, FileName: "contract.pdf", ContentID: "orphan@x"},
		{ID: 3, FileName: "packing-list.xlsx"},
	}
	got := hideEmbedded(atts, body, imageSwap{
		"cid:shown@x":  "https://storage/a",
		"cid:orphan@x": "https://storage/b",
	})
	if len(got) != 2 {
		t.Fatalf("expected contract.pdf and packing-list.xlsx, got %v", names(got))
	}
	for _, want := range []string{"contract.pdf", "packing-list.xlsx"} {
		if !containsName(got, want) {
			t.Errorf("%s went missing from the list", want)
		}
	}
}

// A mail with no embedded pictures must come back exactly as it went in.
func TestAnOrdinaryMailsAttachmentListIsUntouched(t *testing.T) {
	atts := []Attachment{{ID: 1, FileName: "quote.pdf"}, {ID: 2, FileName: "photo.jpg"}}
	got := hideEmbedded(atts, `<p>Attached.</p>`, imageSwap{"cid:x": "https://storage/x"})
	if len(got) != 2 {
		t.Fatalf("an ordinary list was filtered: %v", names(got))
	}
}

func TestNothingIsHiddenWhenThereAreNoIdentifiers(t *testing.T) {
	// The body embeds something, but no stored part answers to it — the exact
	// state of the 116 messages before the backfill. Nothing may be hidden,
	// because hiding here would take the picture off the screen entirely.
	body := `<img src="cid:8E971712@x">`
	atts := []Attachment{{ID: 1, FileName: "image.png"}}
	if got := hideEmbedded(atts, body, nil); len(got) != 1 {
		t.Fatal("an unrepaired message lost its attachment from the list")
	}
}

// A part the body points at but that we could not turn into a URL must stay
// listed. Hiding it would take it off the screen twice: the sanitiser strips
// the unresolved cid: from the body, and this would remove the only other way
// to reach a file that did arrive.
func TestAPartWeCouldNotResolveStaysInTheList(t *testing.T) {
	body := `<img src="cid:logo@x">`
	atts := []Attachment{{ID: 1, FileName: "logo.png", ContentID: "logo@x"}}
	got := hideEmbedded(atts, body, imageSwap{"cid:logo@x": ""})
	if len(got) != 1 {
		t.Fatal("an unresolved part was hidden, leaving the file unreachable")
	}
}

func names(atts []Attachment) []string {
	out := make([]string, 0, len(atts))
	for _, a := range atts {
		out = append(out, a.FileName)
	}
	return out
}

func containsName(atts []Attachment, want string) bool {
	for _, a := range atts {
		if a.FileName == want {
			return true
		}
	}
	return false
}
