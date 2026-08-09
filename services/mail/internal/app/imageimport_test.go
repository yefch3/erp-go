package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOnlyWebAddressesAreAcceptedForAnImage(t *testing.T) {
	ok := []string{
		"https://example.com/logo.png",
		"http://example.com/logo.png",
		"  https://example.com/logo.png  ",
	}
	for _, raw := range ok {
		if _, err := parseImageURL(raw); err != nil {
			t.Errorf("%q was refused: %v", raw, err)
		}
	}
	// Every one of these is a way of asking the server to read something that
	// is not a picture on the public web.
	bad := []string{
		"",
		"   ",
		"logo.png",
		"//example.com/logo.png",
		"javascript:alert(1)",
		"data:image/png;base64,iVBORw0KGgo=",
		"file:///etc/passwd",
		"ftp://example.com/logo.png",
		"gopher://example.com/",
	}
	for _, raw := range bad {
		if _, err := parseImageURL(raw); err == nil {
			t.Errorf("%q was accepted", raw)
		}
	}
}

// The guard that makes this endpoint safe lives in the dialler, and the whole
// point of reusing imageClient() is that it applies here too. A machine inside
// the network that will fetch any URL on request is how the cloud metadata
// service gets read; this test fails the moment somebody swaps in a plain
// http.Client "just to make the test pass".
func TestImportingRefusesAnAddressInsideOurOwnNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(onePixelPNG())
	}))
	defer srv.Close()

	files := &countingFiles{}
	s := &Service{files: files, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	// httptest binds to loopback, which is exactly the class of address the
	// dialler refuses. The server would happily serve a real PNG.
	_, err := s.ImportImageFromURL(context.Background(), 1, srv.URL+"/logo.png", Operator{ID: 1})
	if err == nil {
		t.Fatal("a loopback address was fetched")
	}
	if files.puts != 0 {
		t.Fatalf("nothing should have been stored, saw %d writes", files.puts)
	}
	// The reason came from somebody else's server and may name their hosts.
	if strings.Contains(err.Error(), "127.0.0.1") || strings.Contains(err.Error(), srv.URL) {
		t.Errorf("the address leaked into the message shown to the employee: %v", err)
	}
}

func TestImportRefusesBeforeItFetchesWhenTheAddressIsNotAWebAddress(t *testing.T) {
	files := &countingFiles{}
	s := &Service{files: files, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := s.ImportImageFromURL(
		context.Background(), 1, "file:///etc/passwd", Operator{ID: 1},
	); err == nil {
		t.Fatal("a file:// address was accepted")
	}
	if files.puts != 0 {
		t.Fatalf("nothing should have been stored, saw %d writes", files.puts)
	}
}

func TestTheNameOfAnImportedPictureFollowsTheBytesNotTheAddress(t *testing.T) {
	cases := []struct {
		raw         string
		contentType string
		want        string
	}{
		{"https://example.com/assets/logo.png", "image/png", "logo.png"},
		// Served as PNG from a .jpg path: the extension follows what arrived,
		// or the picker labels it with a lie.
		{"https://example.com/logo.jpg", "image/png", "logo.png"},
		// No filename in the path at all — plenty of logos are served this way.
		{"https://cdn.example.com/", "image/png", "cdn.example.com.png"},
		{"https://cdn.example.com", "image/webp", "cdn.example.com.webp"},
		// A query string is not part of the name.
		{"https://example.com/img?id=44", "image/gif", "img.gif"},
	}
	for _, c := range cases {
		u, err := parseImageURL(c.raw)
		if err != nil {
			t.Fatalf("%q: %v", c.raw, err)
		}
		if got := imageNameFromURL(u, c.contentType); got != c.want {
			t.Errorf("%q → %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestALongPathDoesNotBecomeALongFileName(t *testing.T) {
	u, err := parseImageURL("https://example.com/" + strings.Repeat("a", 300) + ".png")
	if err != nil {
		t.Fatal(err)
	}
	got := imageNameFromURL(u, "image/png")
	if len(got) > 70 {
		t.Fatalf("name is %d characters: %q", len(got), got)
	}
	if !strings.HasSuffix(got, ".png") {
		t.Fatalf("the extension was truncated away: %q", got)
	}
}

// A rich editor left untouched does not produce an empty string; it produces
// "<br>" or an empty paragraph. Storing that gives every mail a signature that
// appends nothing, and the list cannot tell it from a working one.
func TestASignatureThatRendersAsNothingIsRefused(t *testing.T) {
	blank := []string{"", "   ", "<br>", "<p></p>", "<div><br></div>", "<p>&nbsp;</p>"}
	for _, html := range blank {
		if !blankSignature(html) {
			t.Errorf("%q was accepted as a signature", html)
		}
	}
	// A sign-off that is only the company logo is an ordinary signature, and
	// judging by text alone would throw it out.
	real := []string{
		"Best regards",
		`<img src="https://example.com/logo.png" alt="logo">`,
		`<p>Best regards</p><img src="https://example.com/logo.png" alt="logo">`,
	}
	for _, html := range real {
		if blankSignature(html) {
			t.Errorf("%q was refused as blank", html)
		}
	}
}

// Now that every signature written in the editor is HTML, the join is what
// decides whether the logo survives into the mail.
func TestALogoSurvivesIntoAnHTMLBodyAndIsFlattenedOutOfATextOne(t *testing.T) {
	sig := `<p>Best regards</p><img src="https://erp.example.com/api/public/mail-images/abc" alt="Acme">`

	html := joinSignature("<p>Prices attached.</p>", sig, FormatHTML, FormatHTML)
	if !strings.Contains(html, `<img src="https://erp.example.com/api/public/mail-images/abc"`) {
		t.Fatalf("the logo did not reach the HTML body: %q", html)
	}

	// A sender who deliberately chose plain text gets plain text; their format
	// choice decides what is sent, and the alt is what carries the meaning.
	text := joinSignature("Prices attached.", sig, FormatText, FormatHTML)
	if strings.Contains(text, "<img") {
		t.Fatalf("markup leaked into a plain-text mail: %q", text)
	}
}

// countingFiles is storage that records whether anything was written. The
// import path must not reach it when the address is refused.
type countingFiles struct{ puts int }

func (f *countingFiles) PresignPut(context.Context, string) (string, int32, error) {
	return "", 0, nil
}
func (f *countingFiles) PresignGet(context.Context, string, string) (string, error) {
	return "", nil
}
func (f *countingFiles) PresignGetInline(context.Context, string, string) (string, error) {
	return "", nil
}
func (f *countingFiles) Stat(context.Context, string) (int64, string, error) { return 0, "", nil }
func (f *countingFiles) Get(context.Context, string) (io.ReadCloser, error)  { return nil, nil }
func (f *countingFiles) Remove(context.Context, string) error                { return nil }
func (f *countingFiles) Put(context.Context, string, io.Reader, int64, string) error {
	f.puts++
	return nil
}
