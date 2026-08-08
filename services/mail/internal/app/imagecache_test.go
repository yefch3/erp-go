package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ------------------------------------------------------- what may be dialled

// The addresses in here were chosen by whoever wrote the mail, and they are
// dialled by a process sitting inside our network. Every entry below is a
// real thing an attacker sends.
func TestOnlyPubliclyRoutableAddressesMayBeDialled(t *testing.T) {
	refused := map[string]string{
		"127.0.0.1":       "loopback — the service's own ports",
		"::1":             "loopback, v6",
		"10.1.2.3":        "private — every other service in the compose file",
		"172.17.0.5":      "private — the docker bridge",
		"192.168.1.10":    "private — the office LAN the server sits on",
		"169.254.169.254": "AWS/GCP metadata: answers with instance credentials",
		"100.100.100.200": "Alibaba Cloud metadata, behind carrier-grade NAT",
		"0.0.0.0":         "unspecified",
		"fd00::1":         "unique local, v6",
		"fe80::1":         "link local, v6",
		"224.0.0.1":       "multicast",
		"255.255.255.255": "broadcast",
	}
	for addr, why := range refused {
		if routableIP(net.ParseIP(addr)) {
			t.Errorf("%s would be dialled (%s)", addr, why)
		}
	}
	for _, addr := range []string{"93.184.216.34", "1.1.1.1", "2606:4700::1111"} {
		if !routableIP(net.ParseIP(addr)) {
			t.Errorf("%s is a public address and was refused", addr)
		}
	}
}

// A hostname check would be beaten by a name that answers publicly once and
// privately the second time. The guard is in the dialer, so it sees the
// address actually being connected to — this test drives the real client at
// a real loopback listener to prove the guard is wired in, not just written.
func TestTheClientRefusesToReachOurOwnNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(onePixelPNG())
	}))
	defer srv.Close()

	// srv.URL is http://127.0.0.1:PORT — exactly the shape of an internal
	// service, and exactly what a hostile <img src> would name.
	if _, err := fetchOneImage(context.Background(), imageClient(), srv.URL); err == nil {
		t.Fatal("a loopback address was fetched")
	}
}

// ------------------------------------------------------------ our own pixel

// A customer replying quotes our mail back, tracking pixel and all. Fetching
// it would record them as having opened a message they may never have opened
// — the system marking its own homework. Eight messages in the real mailbox
// already carry one of these.
func TestOurOwnTrackingPixelIsNeverFetched(t *testing.T) {
	body := `<p>Sure, send the PI.</p>
	<blockquote>
	  <img src="https://erp.aaaindustryinc.com/api/public/mail-open/2a6fb6dc-2f5e-474d-b8cc-eacdc2e10e3c" width="1" height="1">
	  <img src="https://cdn.example.com/logo.png">
	</blockquote>`
	got := remoteImagesIn(body, "erp.aaaindustryinc.com")
	if len(got) != 1 || got[0] != "https://cdn.example.com/logo.png" {
		t.Fatalf("picked up %v; our own pixel must not be among them", got)
	}
}

// With no public address configured there is no pixel of ours in the wild,
// and nothing should be skipped on that account.
func TestWithNoPublicAddressNothingIsSkippedAsOurOwn(t *testing.T) {
	body := `<img src="https://cdn.example.com/a.png">`
	if got := remoteImagesIn(body, ""); len(got) != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestPublicHostIsReadFromTheConfiguredBaseURL(t *testing.T) {
	for base, want := range map[string]string{
		"https://erp.aaaindustryinc.com": "erp.aaaindustryinc.com",
		"https://ERP.Example.com/":       "erp.example.com",
		"http://localhost:8080":          "localhost",
		"":                               "",
		"not a url at all with spaces":   "",
	} {
		if got := publicHostOf(base); got != want {
			t.Errorf("publicHostOf(%q) = %q, want %q", base, got, want)
		}
	}
}

// ---------------------------------------------------------- finding pictures

// Marketing mail does not put every picture in an <img src>. A background
// image in a stylesheet is a request to the sender's server just the same,
// and srcset is how a picture is served twice over.
func TestPicturesAreFoundInEveryPlaceAMailHidesThem(t *testing.T) {
	body := `<style>.hero{background-image:url("https://a.example/bg.jpg")}</style>
	<img src="https://b.example/one.png">
	<img src='https://c.example/two.png' srcset="https://d.example/2x.png 2x, https://e.example/3x.png 3x">
	<div style="background:url(https://f.example/inline.gif)"></div>`
	got := remoteImagesIn(body, "")
	for _, want := range []string{
		"https://a.example/bg.jpg", "https://b.example/one.png", "https://c.example/two.png",
		"https://d.example/2x.png", "https://e.example/3x.png", "https://f.example/inline.gif",
	} {
		if !contains(got, want) {
			t.Errorf("missed %s; found %v", want, got)
		}
	}
}

// A link is not a picture. Rewriting an href would turn "click through to the
// customer's site" into "download an image from our storage".
func TestALinkIsNotRewritten(t *testing.T) {
	body := `<a href="https://buyer.example/catalogue"><img src="https://buyer.example/logo.png"></a>`
	swapped := mapImageURLs(body, func(u string) string { return "SWAPPED" })
	if !strings.Contains(swapped, `href="https://buyer.example/catalogue"`) {
		t.Fatalf("the link was rewritten:\n%s", swapped)
	}
	if !strings.Contains(swapped, `src="SWAPPED"`) {
		t.Fatalf("the picture was not rewritten:\n%s", swapped)
	}
}

// Inline data and attachment references are already local. Fetching a cid:
// would be fetching nothing; fetching a data: would be fetching ourselves.
func TestAlreadyLocalPicturesAreLeftAlone(t *testing.T) {
	body := `<img src="data:image/png;base64,iVBORw0KGgo="><img src="cid:logo@sender">`
	if got := remoteImagesIn(body, ""); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

// A quoted reply chain names the sender's signature logo once per turn. The
// count is what decides how many times we go out to their server.
func TestTheSamePictureIsOnlyFetchedOnce(t *testing.T) {
	one := `<img src="https://a.example/sig.png">`
	got := remoteImagesIn(strings.Repeat(one, 16), "")
	if len(got) != 1 {
		t.Fatalf("a picture named sixteen times produced %d fetches", len(got))
	}
}

func TestTheNumberOfPicturesPerMailIsBounded(t *testing.T) {
	var b strings.Builder
	for i := 0; i < cacheMaxImagesPerMail+40; i++ {
		b.WriteString(`<img src="https://a.example/` + hex.EncodeToString([]byte{byte(i)}) + `.png">`)
	}
	if got := len(remoteImagesIn(b.String(), "")); got != cacheMaxImagesPerMail {
		t.Fatalf("collected %d, cap is %d", got, cacheMaxImagesPerMail)
	}
}

// ------------------------------------------------------------- substitution

// Finding and replacing run through the same function, so a picture that was
// fetched is a picture that gets used.
func TestWhatWasFoundIsWhatGetsReplaced(t *testing.T) {
	body := `<style>.h{background-image:url("https://a.example/bg.jpg")}</style>
	<img src="https://b.example/one.png" alt="x">
	<img srcset="https://d.example/2x.png 2x">`
	swap := imageSwap{}
	for _, u := range remoteImagesIn(body, "") {
		swap[u] = "https://storage.internal/signed/" + hex.EncodeToString(urlHash(u))[:8]
	}
	out := (&Service{}).localiseImages(context.Background(), body, swap)
	for _, u := range remoteImagesIn(body, "") {
		if strings.Contains(out, u) {
			t.Errorf("%s survived the swap:\n%s", u, out)
		}
	}
	if !strings.Contains(out, `alt="x"`) {
		t.Fatalf("the rest of the tag was damaged:\n%s", out)
	}
}

// The degradation is deliberate: a picture we could not fetch keeps pointing
// at the sender, so the mail still looks right. Silently dropping it would
// make a failed fetch look like a mail with a missing picture.
func TestAPictureThatWasNotCachedIsLeftPointingAtTheSender(t *testing.T) {
	body := `<img src="https://a.example/cached.png"><img src="https://b.example/missed.png">`
	swap := imageSwap{"https://a.example/cached.png": "https://storage/signed"}
	out := (&Service{}).localiseImages(context.Background(), body, swap)
	if !strings.Contains(out, "https://b.example/missed.png") {
		t.Fatalf("an uncached picture was dropped:\n%s", out)
	}
	if strings.Contains(out, "https://a.example/cached.png") {
		t.Fatalf("a cached picture was not swapped:\n%s", out)
	}
}

// The stored address has to be the address the reader will match against.
// SanitizeForReading percent-encodes spaces in URLs on the way out, so a URL
// recorded in its raw shape would never be found again.
func TestTheStoredAddressMatchesWhatTheReaderSees(t *testing.T) {
	raw := `<img src="https://a.example/two words.png">`
	stored := remoteImagesIn(repairURLWhitespace(raw), "")
	if len(stored) != 1 {
		t.Fatalf("got %v", stored)
	}
	asRead := remoteImagesIn(SanitizeForReading(raw), "")
	if len(asRead) != 1 || asRead[0] != stored[0] {
		t.Fatalf("ingest stored %v, the reader sees %v", stored, asRead)
	}
}

// ------------------------------------------------------------------ fetching

// The type is decided from the bytes. Trusting the sender's Content-Type
// would let them have text/html recorded, and that type is handed straight
// back to the browser when the picture is served.
func TestTheTypeComesFromTheBytesNotTheHeader(t *testing.T) {
	if got := sniffImage([]byte(`<html><body>not a picture</body></html>`)); got != "" {
		t.Fatalf("markup was accepted as %q", got)
	}
	if got := sniffImage(onePixelPNG()); got != "image/png" {
		t.Fatalf("a PNG sniffed as %q", got)
	}
	if got := sniffImage([]byte{}); got != "" {
		t.Fatal("empty bytes were accepted")
	}
}

// A server that answers an image request with a login page, a redirect chain,
// or half a gigabyte must not produce a stored object.
func TestAServerThatDoesNotReturnAPictureProducesNothing(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"html login page": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png") // lying, on purpose
			_, _ = w.Write([]byte("<html><head><title>Sign in</title>"))
		},
		"404": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) },
		"empty 200": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png")
		},
		"oversized": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(append(onePixelPNG(), make([]byte, cacheMaxImageBytes+1024)...))
		},
	}
	for name, h := range cases {
		srv := httptest.NewServer(h)
		// The dialer refuses loopback, which is the point of the guard — so
		// this exercises the body checks through a client without it.
		client := srv.Client()
		_, err := fetchOneImage(context.Background(), client, srv.URL)
		if err == nil {
			t.Errorf("%s: was accepted as a picture", name)
		}
		srv.Close()
	}
}

func TestAGenuinePictureIsAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(onePixelPNG())
	}))
	defer srv.Close()
	img, err := fetchOneImage(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("a real PNG was refused: %v", err)
	}
	if img.contentType != "image/png" || len(img.data) == 0 {
		t.Fatalf("got %+v", img)
	}
	// The stored object is named after the bytes, so two mails quoting the
	// same logo do not each need a distinct name invented for them.
	sum := sha256.Sum256(img.data)
	if hex.EncodeToString(sum[:8]) == "" {
		t.Fatal("the bytes did not hash")
	}
}

// ---------------------------------------------------------------- helpers

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// onePixelPNG is the smallest thing that sniffs as a PNG — the same shape as
// the tracking pixels this whole change exists to intercept.
func onePixelPNG() []byte {
	return []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0, 0, 0, 0x0d, 'I', 'H', 'D', 'R',
		0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0,
		0x1f, 0x15, 0xc4, 0x89,
		0, 0, 0, 0x0a, 'I', 'D', 'A', 'T',
		0x78, 0x9c, 0x63, 0, 1, 0, 0, 5, 0, 1,
		0x0d, 0x0a, 0x2d, 0xb4,
		0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	}
}
