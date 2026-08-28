package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"os"
	"strconv"
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
				"section_ref": "HRC", "source_row": "1", "source_size": "1.10 x 1200", "source_quantity": "1250",
			}},
			"image_audit": map[string]any{
				"detail_row_count": 1,
				"sections": []map[string]any{{
					"section_ref": "HRC", "detail_row_count": 1, "stated_total": "1250", "quantity_unit": "MT",
					"shared_values": map[string]string{"product": "HRC"},
				}},
			},
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

// Opt-in regression against the real model. The fixture is small but catches
// the failures that a mocked response cannot: skipped visual rows, a section's
// COIL ID leaking into another section, and Peso being mistaken for a product
// or left without its MT unit.
func TestLiveImageInquiryFixture(t *testing.T) {
	if os.Getenv("ERP_LIVE_OPENAI") != "1" {
		t.Skip("set ERP_LIVE_OPENAI=1 to run the real image extraction")
	}
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Fatal("OPENAI_API_KEY is required")
	}
	image, err := os.ReadFile("../../../../../_test_case/c88b244ae567a78ca04b9a232cc94a66.jpg")
	if err != nil {
		t.Fatal(err)
	}
	client := NewTableExtractor(key, os.Getenv("OPENAI_BASE_URL"), os.Getenv("OPENAI_MODEL"), 4*time.Minute)
	got, err := client.Extract(t.Context(), app.TableExtractionInput{
		FileName: "c88b244ae567a78ca04b9a232cc94a66.jpg", ContentType: "image/jpeg",
		FileData: image, Locale: "zh", SafetyID: "live-image-regression",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Inquiry.Items) != 23 {
		t.Fatalf("detail rows = %d, want 23", len(got.Inquiry.Items))
	}
	if got.Inquiry.ImageAudit == nil || len(got.Inquiry.ImageAudit.Sections) != 3 {
		t.Fatalf("image audit = %#v, want 3 sections", got.Inquiry.ImageAudit)
	}
	wantSections := []struct {
		name, total, coilID, surface, coating string
		standard                              []string
		thickness, width, quantities          []string
	}{
		{"HRC", "1250", "762", "", "", []string{"ASTM A36", "JIS G-3132 SPHT-1"},
			[]string{"1.10", "1.20", "1.30", "1.40", "1.60", "1.70", "1.90", "2.38", "2.85"},
			[]string{"", "", "", "", "", "", "", "", ""},
			[]string{"100", "100", "100", "150", "100", "250", "250", "100", "100"}},
		{"CRC", "550", "610", "GENERAL OILED", "", []string{"JIS G 3141 SPCC SD"},
			[]string{"0.50", "0.55", "0.65", "0.70", "0.75", "0.85", "1.10"},
			[]string{"1200", "1200", "1200", "1200", "1200", "1200", "1200"},
			[]string{"50", "50", "150", "150", "50", "50", "50"}},
		{"GALVANIZED", "1300", "610", "REGULAR SPANGLE", "Z120", []string{"ASTM A653 CS-B"},
			[]string{"0.85", "1.10", "1.40", "1.70", "1.90", "2.38", "2.85"},
			[]string{"1200", "1200", "1200", "1200", "1200", "1200", "1200"},
			[]string{"50", "200", "350", "250", "250", "100", "100"}},
	}
	bySection := map[string][]map[string]string{}
	for _, item := range got.Inquiry.Items {
		bySection[item["section_ref"]] = append(bySection[item["section_ref"]], item)
	}
	auditBySection := map[string]app.ImageAuditSection{}
	for _, section := range got.Inquiry.ImageAudit.Sections {
		auditBySection[section.SectionRef] = section
	}
	for _, want := range wantSections {
		items := bySection[want.name]
		if len(items) != len(want.thickness) {
			t.Fatalf("%s rows = %d, want %d; sections=%v", want.name, len(items), len(want.thickness), mapsKeys(bySection))
		}
		if actual := auditBySection[want.name].StatedTotal; !sameImageDecimal(actual, want.total) {
			t.Fatalf("%s stated total = %q, want %s", want.name, actual, want.total)
		}
		for i, item := range items {
			if item["source_row"] != strconv.Itoa(i+1) || !strings.EqualFold(item["quantity_unit"], "MT") {
				t.Fatalf("%s row %d identity/unit = %q/%q", want.name, i+1, item["source_row"], item["quantity_unit"])
			}
			if canonicalImageFact(item["coil_id"]) != want.coilID {
				t.Fatalf("%s row %d coil_id = %q, want %s", want.name, i+1, item["coil_id"], want.coilID)
			}
			if !sameImageDecimal(item["coil_weight"], "10.50") {
				t.Fatalf("%s row %d coil_weight = %q", want.name, i+1, item["coil_weight"])
			}
			if !sameImageDecimal(item["thickness"], want.thickness[i]) || !sameImageDecimal(item["width"], want.width[i]) || !sameImageDecimal(item["quantity"], want.quantities[i]) {
				t.Fatalf("%s row %d size/quantity = %q x %q / %q, want %s x %s / %s", want.name, i+1, item["thickness"], item["width"], item["quantity"], want.thickness[i], want.width[i], want.quantities[i])
			}
			standard := item["material_standard"] + " " + item["grade"]
			for _, token := range want.standard {
				if !strings.Contains(normalizeImageStandard(standard), normalizeImageStandard(token)) {
					t.Fatalf("%s row %d standard = %q, missing %q", want.name, i+1, standard, token)
				}
			}
			if want.surface != "" && !strings.Contains(strings.ToUpper(item["surface_requirement"]), want.surface) {
				t.Fatalf("%s row %d surface = %q", want.name, i+1, item["surface_requirement"])
			}
			if want.coating != "" && !strings.Contains(strings.ToUpper(item["coating"]), want.coating) {
				t.Fatalf("%s row %d coating = %q", want.name, i+1, item["coating"])
			}
		}
	}
}

func sameImageDecimal(got, want string) bool {
	got, want = strings.TrimSpace(got), strings.TrimSpace(want)
	if got == "" || want == "" {
		return got == want
	}
	a, aOK := new(big.Rat).SetString(got)
	b, bOK := new(big.Rat).SetString(want)
	return aOK && bOK && a.Cmp(b) == 0
}

func normalizeImageStandard(value string) string {
	return strings.NewReplacer(" ", "", "-", "", "/", "").Replace(strings.ToUpper(value))
}

func canonicalImageFact(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSuffix(fields[0], ".0")
}

func mapsKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
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

func TestExtractorRequiresEveryPrecountedSpreadsheetRow(t *testing.T) {
	refs := []string{"PRODUCTS!3", "COILS!3"}
	var captured map[string]any
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		extracted, _ := json.Marshal(map[string]any{
			"title": "t", "summary": "s",
			"items": []map[string]string{
				{"source_ref": "PRODUCTS!3", "product": "sheet", "quantity": "1", "quantity_unit": "MT"},
				{"source_ref": "COILS!3", "product": "coil", "quantity": "2", "quantity_unit": "MT"},
			},
		})
		responseBytes, _ := json.Marshal(map[string]any{
			"model": "gpt-5.6-luna", "status": "completed",
			"output": []any{map[string]any{"type": "message", "content": []any{
				map[string]any{"type": "output_text", "text": string(extracted)},
			}}},
		})
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(responseBytes)), Request: r}, nil
	})}
	client := NewTableExtractor("test-key", "https://api.test/v1", "gpt-5.6-luna", time.Second).WithHTTPClient(httpClient)
	got, err := client.Extract(t.Context(), app.TableExtractionInput{Text: "two rows", SourceRefs: refs})
	if err != nil || len(got.Workbook.Sheets[0].Rows) != 2 {
		t.Fatalf("valid source coverage: workbook=%#v err=%v", got.Workbook, err)
	}
	format := captured["text"].(map[string]any)["format"].(map[string]any)
	properties := format["schema"].(map[string]any)["properties"].(map[string]any)["items"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	if got := properties["source_ref"].(map[string]any)["enum"]; len(got.([]any)) != 2 {
		t.Fatalf("source_ref enum = %#v", got)
	}
	input := captured["input"].([]any)[0].(map[string]any)["content"].([]any)
	prompt := input[0].(map[string]any)["text"].(string)
	for _, want := range []string{"context_sheets", "context_blocks", "Context blocks never create requested items", "length_or_form must contain only a canonical decimal length in millimetres", "rectangular tube/REC/RECT", "custom.height_or_leg2", "inch fractions"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("spreadsheet prompt misses %q: %s", want, prompt)
		}
	}
	if captured["stream"] != true {
		t.Fatalf("stream = %#v, want true", captured["stream"])
	}

	bad := app.ExtractedInquiry{Items: []map[string]string{{"source_ref": "PRODUCTS!3"}}}
	if err := validateSourceRefs(bad, refs); err == nil {
		t.Fatal("missing source row must fail validation")
	}
}

func TestImageAuditRejectsMissingRowsWrongTotalsAndSharedFacts(t *testing.T) {
	columns := app.SystemInquiryColumns()
	valid := app.ExtractedInquiry{
		Items: []map[string]string{
			{"section_ref": "HRC", "source_row": "1", "product": "HRC", "coil_id": "762", "quantity": "100"},
			{"section_ref": "HRC", "source_row": "2", "product": "HRC", "coil_id": "762", "quantity": "150"},
		},
		ImageAudit: &app.ImageExtractionAudit{DetailRowCount: 2, Sections: []app.ImageAuditSection{{
			SectionRef: "HRC", DetailRowCount: 2, StatedTotal: "250",
			SharedValues: map[string]string{"product": "HRC", "coil_id": "762"},
		}}},
	}
	if err := validateImageAudit(valid, columns); err != nil {
		t.Fatalf("valid audit: %v", err)
	}

	missing := valid
	missing.Items = missing.Items[:1]
	if err := validateImageAudit(missing, columns); err == nil {
		t.Fatal("missing image row must fail")
	}

	wrongTotal := valid
	wrongTotal.Items = append([]map[string]string(nil), valid.Items...)
	wrongTotal.Items[1] = map[string]string{"section_ref": "HRC", "source_row": "2", "product": "HRC", "coil_id": "762", "quantity": "140"}
	if err := validateImageAudit(wrongTotal, columns); err == nil {
		t.Fatal("wrong section total must fail")
	}

	wrongShared := valid
	wrongShared.Items = append([]map[string]string(nil), valid.Items...)
	wrongShared.Items[1] = map[string]string{"section_ref": "HRC", "source_row": "2", "product": "HRC", "coil_id": "610", "quantity": "150"}
	if err := validateImageAudit(wrongShared, columns); err == nil {
		t.Fatal("wrong shared coil id must fail")
	}
}

func TestImageCellAnchorsPreventCrossSectionWidthLeak(t *testing.T) {
	items := []map[string]string{
		{"source_size": "1.10", "source_quantity": "100", "thickness": "1.10", "width": "1200"},
		{"source_size": "0.50 X 1200", "source_quantity": "50 MT", "thickness": "0.50", "width": ""},
	}
	applyImageCellAnchors(items)
	if items[0]["thickness"] != "1.10" || items[0]["width"] != "" || items[0]["quantity"] != "100" {
		t.Fatalf("one-number size was not anchored: %#v", items[0])
	}
	if items[1]["thickness"] != "0.50" || items[1]["width"] != "1200" || items[1]["quantity"] != "50" {
		t.Fatalf("two-number size was not anchored: %#v", items[1])
	}
}

func TestDecodeStreamingCompletedResponse(t *testing.T) {
	stream := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"status":"in_progress"}}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"model":"gpt-test","status":"completed","usage":{"input_tokens":12,"output_tokens":4},"output":[{"type":"message","content":[{"type":"output_text","text":"{\"title\":\"t\",\"summary\":\"s\",\"items\":[]}"}]}]}}`,
		"",
	}, "\n")
	got, err := decodeResponse(strings.NewReader(stream), "text/event-stream")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "completed" || got.Model != "gpt-test" || got.Usage.InputTokens != 12 {
		t.Fatalf("decoded stream = %#v", got)
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
