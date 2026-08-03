package blobstore

import "testing"

func TestASCIIFallbackKeepsTheParameterLegal(t *testing.T) {
	// The quoted form has to stay ASCII and must not contain the quote or the
	// backslash, either of which would end the parameter early and let the
	// rest of the file name leak into the header as syntax.
	cases := map[string]string{
		"报价单 2026.pptx":       "___ 2026.pptx",
		`quote "final".pdf`:   "quote _final_.pdf",
		`back\slash.docx`:     "back_slash.docx",
		"ordinary-name_1.xlsx": "ordinary-name_1.xlsx",
	}
	for in, want := range cases {
		if got := asciiFallback(in); got != want {
			t.Errorf("asciiFallback(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestASCIIFallbackNeverReturnsEmpty(t *testing.T) {
	// A name that is entirely non-ASCII would otherwise produce filename="",
	// which some clients treat as "no name" and others as an error.
	if got := asciiFallback("报价单"); got == "" {
		t.Fatal("an all-non-ASCII name must still yield something to save as")
	}
}
