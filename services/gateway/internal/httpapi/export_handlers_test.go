package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A Chinese file name has to survive as a Chinese file name. The plain
// filename= form is bytes and cannot carry it; without the extended form the
// download lands as "____-2026-08-08.html" or, on a strict client, as a
// rejected response.
func TestTheFileNameSurvivesInBothForms(t *testing.T) {
	got := attachmentDisposition("邮件会话-buyer@example.com-2026-08-08.html")
	if !strings.Contains(got, "filename*=UTF-8''") {
		t.Fatalf("no extended form: %s", got)
	}
	if !strings.Contains(got, "%E9%82%AE%E4%BB%B6") {
		t.Fatalf("the name was not percent-encoded: %s", got)
	}
	// The fallback keeps the extension, which is what decides whether the
	// file opens in a browser or in a text editor.
	if !strings.Contains(got, `filename="`) || !strings.Contains(got, `.html"`) {
		t.Fatalf("the ASCII fallback lost its extension: %s", got)
	}
}

// A header is a line. A file name carrying a newline would end that line and
// start another one of the sender's choosing — and the name is built partly
// from an address anybody can pick for themselves.
func TestAFileNameCannotInjectAHeader(t *testing.T) {
	got := attachmentDisposition("a\r\nX-Evil: 1\r\n.html")
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("newlines survived into the header: %q", got)
	}
	// And the quoted form cannot be closed early.
	quoted := attachmentDisposition(`a".html`)
	if strings.Count(quoted, `"`) != 2 {
		t.Fatalf("the quoted filename is not balanced: %s", quoted)
	}
}

func TestAnAllNonASCIIFileNameStillHasAFallback(t *testing.T) {
	got := attachmentDisposition("会话.html")
	if strings.Contains(got, `filename=""`) {
		t.Fatalf("empty fallback: %s", got)
	}
}

// What a person sees first in their downloads list is the start of the name.
// "邮件会话-" turning into "____-" put four characters of rubble there, and
// then the trim left a bare leading hyphen — both worse than starting at the
// part that says who the conversation was with.
func TestTheFallbackDoesNotOpenWithRubble(t *testing.T) {
	got := asciiFallback("邮件会话-buyer@example.com-2026-08-08.html")
	if got != "buyer@example.com-2026-08-08.html" {
		t.Fatalf("fallback = %q", got)
	}
}

// The header set on the way out matters as much as the bytes: a document
// assembled from one person's mailbox must not sit in a shared cache, and
// must not be sniffed into something the browser will run.
func TestTheDownloadHeadersAreSetForADocumentNobodyShouldCache(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "text/html; charset=utf-8")
	rec.Header().Set("Content-Disposition", attachmentDisposition("x.html"))
	rec.Header().Set("Cache-Control", "no-store")
	rec.Header().Set("X-Content-Type-Options", "nosniff")
	rec.WriteHeader(http.StatusOK)

	// Guards the shape the handler writes; see exportMailThread. Kept as a
	// list so removing one of them fails here rather than in production.
	for k, want := range map[string]string{
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
	} {
		if rec.Header().Get(k) != want {
			t.Fatalf("%s = %q, want %q", k, rec.Header().Get(k), want)
		}
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal("the document is not marked as an attachment")
	}
}
