package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/app"
)

func TestExtractorUsesStructuredResponsesForTextAndRealImageSample(t *testing.T) {
	image, err := os.ReadFile("../../../../../_test_case/c88b244ae567a78ca04b9a232cc94a66.jpg")
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile("../../../../../_test_case/email content.txt")
	if err != nil {
		t.Fatal(err)
	}

	var requests []map[string]any
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatal("missing bearer key")
		}
		body, _ := io.ReadAll(r.Body)
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
		response := `{"model":"gpt-5.6-luna","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"{\"title\":\"Extracted\",\"sheets\":[{\"name\":\"Table\",\"summary\":\"\",\"columns\":[\"Item\",\"Value\"],\"column_types\":[\"string\",\"number\"],\"rows\":[[\"A\",\"1250\"]]}]}"}]}]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(response)), Request: r,
		}, nil
	})}

	client := NewTableExtractor("test-key", "https://api.test/v1", "gpt-5.6-luna", time.Second).WithHTTPClient(httpClient)
	inputs := []app.TableExtractionInput{
		{Text: string(text), Locale: "en", SafetyID: "test-user"},
		{FileName: "sample.jpg", ContentType: "image/jpeg", FileData: image, Locale: "zh", SafetyID: "test-user"},
	}
	for _, input := range inputs {
		book, model, err := client.Extract(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		if model != "gpt-5.6-luna" || len(book.Sheets) != 1 {
			t.Fatalf("unexpected result: %#v %s", book, model)
		}
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d", len(requests))
	}
	for _, request := range requests {
		if request["store"] != false {
			t.Fatal("source must not be stored")
		}
		format := request["text"].(map[string]any)["format"].(map[string]any)
		if format["type"] != "json_schema" || format["strict"] != true {
			t.Fatal("structured output not strict")
		}
	}
	encoded, _ := json.Marshal(requests[1])
	requestText := string(encoded)
	if !strings.Contains(requestText, `"detail":"original"`) {
		t.Fatal("image detail is not original")
	}
	if !strings.Contains(requestText, "data:image/png;base64,") {
		t.Fatal("image data URL did not use the sample's real PNG media type")
	}
}

func TestImageMediaTypePrefersBytesOverMailMetadata(t *testing.T) {
	png, err := os.ReadFile("../../../../../_test_case/c88b244ae567a78ca04b9a232cc94a66.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if got := imageMediaType("wrong.jpg", "image/jpeg", png); got != "image/png" {
		t.Fatalf("media type = %q, want image/png", got)
	}
	if !isImage("attachment.bin", "application/octet-stream", png) {
		t.Fatal("PNG signature was not recognized without a useful name or content type")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExtractorRejectsMissingAPIKeyWithoutNetwork(t *testing.T) {
	client := NewTableExtractor("", "", "", time.Second)
	_, _, err := client.Extract(t.Context(), app.TableExtractionInput{Text: "a,b\n1,2"})
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("err = %v", err)
	}
}
