package openai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/app"
)

// This is intentionally opt-in: it calls the real model and consumes tokens.
// The fixtures cover selected body text, an HTML attachment, a chat screenshot,
// and a multi-page image-only PDF. Row count is the first invariant because a
// polished workbook with missing request lines is still an incorrect result.
func TestLiveNonExcelInquiryFixtures(t *testing.T) {
	if os.Getenv("ERP_LIVE_OPENAI") != "1" {
		t.Skip("set ERP_LIVE_OPENAI=1 to run real non-Excel extraction")
	}
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Fatal("OPENAI_API_KEY is required")
	}

	fixtureDir := "../../../../../_test_case/non_excel"
	expectedData, err := os.ReadFile(filepath.Join(fixtureDir, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var expected struct {
		Cases []struct {
			File         string `json:"file"`
			ExpectedRows int    `json:"expected_rows"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(expectedData, &expected); err != nil {
		t.Fatal(err)
	}

	rowsByFile := make(map[string]int, len(expected.Cases))
	for _, testCase := range expected.Cases {
		if testCase.ExpectedRows > 0 {
			rowsByFile[testCase.File] = testCase.ExpectedRows
		}
	}

	client := NewTableExtractor(key, os.Getenv("OPENAI_BASE_URL"), os.Getenv("OPENAI_MODEL"), 6*time.Minute)
	tests := []struct {
		name, fileName, contentType string
		selectedText                bool
	}{
		{"forwarded text body", "01_forwarded_email_body.txt", "text/plain", true},
		{"marked-up HTML", "02_revision_with_markup.html", "text/html", false},
		{"mobile chat image", "03_mobile_chat_inquiry.jpg", "image/jpeg", false},
		{"scanned multi-page PDF", "04_scanned_multipage_rfq.pdf", "application/pdf", false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(fixtureDir, testCase.fileName))
			if err != nil {
				t.Fatal(err)
			}
			input := app.TableExtractionInput{
				FileName: testCase.fileName, ContentType: testCase.contentType,
				Locale: "zh", SafetyID: "live-non-excel-regression",
			}
			if testCase.selectedText {
				input.Text = string(data)
			} else {
				input.FileData = data
			}
			got, err := client.Extract(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			wantRows := rowsByFile[testCase.fileName]
			if len(got.Inquiry.Items) != wantRows {
				t.Fatalf("detail rows = %d, want %d", len(got.Inquiry.Items), wantRows)
			}
			t.Logf("model=%s rows=%d input_tokens=%d output_tokens=%d", got.Model, len(got.Inquiry.Items), got.Usage.InputTokens, got.Usage.OutputTokens)
		})
	}
}
