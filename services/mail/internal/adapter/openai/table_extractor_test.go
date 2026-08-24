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
		extracted, _ := json.Marshal(map[string]any{
			"title": "Extracted", "summary": "sample",
			"items": []map[string]string{{
				"product": "HRC", "material_standard": "ASTM A36", "thickness": "1.10",
				"width": "1200", "quantity_unit": "MT", "quantity": "1250",
			}},
		})
		responseBytes, _ := json.Marshal(map[string]any{
			"model": "gpt-5.6-luna", "status": "completed",
			"output": []any{map[string]any{"type": "message", "content": []any{
				map[string]any{"type": "output_text", "text": string(extracted)},
			}}},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(responseBytes)), Request: r,
		}, nil
	})}

	client := NewTableExtractor("test-key", "https://api.test/v1", "gpt-5.6-luna", time.Second).WithHTTPClient(httpClient)
	inputs := []app.TableExtractionInput{
		{Text: string(text), Locale: "en", SafetyID: "test-user"},
		{FileName: "sample.jpg", ContentType: "image/jpeg", FileData: image, Locale: "zh", SafetyID: "test-user"},
	}
	for _, input := range inputs {
		got, err := client.Extract(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		book, model := got.Workbook, got.Model
		if model != "gpt-5.6-luna" || len(book.Sheets) != 1 {
			t.Fatalf("unexpected result: %#v %s", book, model)
		}
		if got := book.Sheets[0].Columns; got[len(got)-3] != "数量" || got[len(got)-2] != "单价" || got[len(got)-1] != "总价" {
			t.Fatalf("fixed price columns = %v", got[len(got)-3:])
		}
		// 询盘阶段模型不给单价（提示词写死了），所以总价该是空的——不是
		// 一个指着空单价格子的算式。
		cols := len(book.Sheets[0].Columns)
		if got := book.Sheets[0].Rows[0][cols-2]; got != "" {
			t.Fatalf("unit price must stay blank at inquiry stage, got %q", got)
		}
		if got := book.Sheets[0].Rows[0][cols-1]; got != "" {
			t.Fatalf("total must stay blank when there is no unit price, got %q", got)
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
		schema := format["schema"].(map[string]any)
		items := schema["properties"].(map[string]any)["items"].(map[string]any)
		if _, ok := items["items"].(map[string]any)["properties"].(map[string]any)["quantity"]; !ok {
			t.Fatal("fixed inquiry schema has no quantity")
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

func TestExtractorFollowsTemplateColumns(t *testing.T) {
	columns := []app.InquiryColumn{
		{FieldKey: "product", DisplayName: "品名", DataType: "TEXT", IsRequired: true},
		{FieldKey: "quantity", DisplayName: "需求数量", DataType: "NUMBER", IsRequired: true},
		{FieldKey: "quantity_unit", DisplayName: "计量单位", DataType: "TEXT", IsRequired: true},
		{FieldKey: "custom.customer_part_no", DisplayName: "客户料号", DataType: "TEXT"},
	}
	var captured map[string]any
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		extracted, _ := json.Marshal(map[string]any{
			"title": "t", "summary": "s",
			"items": []map[string]string{{
				"product": "镀锌卷", "quantity": "25", "quantity_unit": "MT", "custom.customer_part_no": "CP-99887",
			}},
		})
		responseBytes, _ := json.Marshal(map[string]any{
			"model": "gpt-5.6-luna", "status": "completed",
			"output": []any{map[string]any{"type": "message", "content": []any{
				map[string]any{"type": "output_text", "text": string(extracted)},
			}}},
		})
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(bytes.NewReader(responseBytes)), Request: r,
		}, nil
	})}

	client := NewTableExtractor("test-key", "https://api.test/v1", "gpt-5.6-luna", time.Second).WithHTTPClient(httpClient)
	got, err := client.Extract(t.Context(), app.TableExtractionInput{Text: "询价：镀锌卷 25MT，料号 CP-99887", Columns: columns})
	if err != nil {
		t.Fatal(err)
	}
	sheet := got.Workbook.Sheets[0]
	if strings.Join(sheet.Columns, ",") != "品名,需求数量,计量单位,客户料号" {
		t.Fatalf("columns should come from the template, got %v", sheet.Columns)
	}
	if got := strings.Join(sheet.Rows[0], ","); got != "镀锌卷,25,MT,CP-99887" {
		t.Fatalf("row = %q", got)
	}
	schema := captured["text"].(map[string]any)["format"].(map[string]any)["schema"].(map[string]any)
	items := schema["properties"].(map[string]any)["items"].(map[string]any)["items"].(map[string]any)
	if _, ok := items["properties"].(map[string]any)["custom.customer_part_no"]; !ok {
		t.Fatal("schema should carry the template's custom column")
	}
	prompt := captured["input"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(prompt, "custom.customer_part_no") || !strings.Contains(prompt, "客户料号") {
		t.Fatal("prompt should describe the template columns")
	}
}

func TestExtractorRejectsMissingAPIKeyWithoutNetwork(t *testing.T) {
	client := NewTableExtractor("", "", "", time.Second)
	_, err := client.Extract(t.Context(), app.TableExtractionInput{Text: "a,b\n1,2"})
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("err = %v", err)
	}
}

// 用量是计费的唯一依据，而它最容易悄悄失效：字段名改了、模型换了、
// 中间加了层代理，Extract 照样返回结果，只是账上从此是零。
func TestExtractorReportsTokenUsageIncludingOnFailure(t *testing.T) {
	reply := func(outputText string) []byte {
		b, _ := json.Marshal(map[string]any{
			"model": "gpt-5.6-luna", "status": "completed",
			"usage": map[string]any{"input_tokens": 12345, "output_tokens": 678},
			"output": []any{map[string]any{"type": "message", "content": []any{
				map[string]any{"type": "output_text", "text": outputText},
			}}},
		})
		return b
	}
	newClient := func(body []byte) *TableExtractor {
		hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(body)), Request: r,
			}, nil
		})}
		return NewTableExtractor("test-key", "https://api.test/v1", "gpt-5.6-luna", time.Second).WithHTTPClient(hc)
	}

	valid, _ := json.Marshal(map[string]any{
		"title": "t", "summary": "s",
		"items": []map[string]string{{"product": "HRC", "quantity": "10", "quantity_unit": "MT"}},
	})
	got, err := newClient(reply(string(valid))).Extract(t.Context(), app.TableExtractionInput{Text: "询价"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage.InputTokens != 12345 || got.Usage.OutputTokens != 678 {
		t.Fatalf("成功时该带回用量，实际 %+v", got.Usage)
	}

	// 模型答了、钱花了，只是答出来的东西解不开。这几次照样计费——漏掉它们，
	// 我们的账和模型厂的账单就对不上。
	broken, err := newClient(reply("这不是 JSON")).Extract(t.Context(), app.TableExtractionInput{Text: "询价"})
	if err == nil {
		t.Fatal("解不开的返回该报错")
	}
	if broken.Usage.InputTokens != 12345 || broken.Usage.OutputTokens != 678 {
		t.Fatalf("失败时也必须带回用量，实际 %+v", broken.Usage)
	}
}
