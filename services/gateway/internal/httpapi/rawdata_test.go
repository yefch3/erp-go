package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

// encoderEnvelope is how these responses were written before writeRawData.
func encoderEnvelope(raw string) []byte {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(envelope{Success: true, Data: json.RawMessage(raw)})
	return buf.Bytes()
}

// What the services send is their own json.Marshal output, so that is what
// is fed in here: everything json.Marshal can be asked to write, including
// the things the encoder's second pass would rewrite if they were there raw
// (HTML characters, U+2028/2029, whitespace inside an embedded RawMessage).
func TestRawDataIsWhatTheEncoderWrote(t *testing.T) {
	type product struct {
		Product      string            `json:"product"`
		CustomFields map[string]string `json:"customFields"`
	}
	values := []any{
		map[string]any{"items": []any{}, "total": 0},
		map[string]any{"items": nil},
		map[string]string{"html": `<b>Steel & "Coil"</b>`, "seps": "a\u2028b\u2029c", "cn": "热轧卷 · 2.0mm", "emoji": "🚢", "ctl": "tab\there\nnew\x01", "bad": "\xff\xfe"},
		map[string]any{"embedded": json.RawMessage(`{ "a" : [ 1 , 2 ] ,"b":"<x>" }`), "n": 12345678901234567, "f": 1e21, "small": 0.000001, "ok": true, "none": nil},
		[]product{{Product: "Hot rolled coil", CustomFields: map[string]string{"grade": "SS400", "tolerance": "+/-0.15mm"}}},
		"just a string", 42, true, nil, []int{},
	}
	for i, v := range values {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		writeRawData(rec, string(raw))
		if want := encoderEnvelope(string(raw)); !bytes.Equal(rec.Body.Bytes(), want) {
			t.Fatalf("value %d: bytes differ\nencoder %q\nspliced %q", i, want, rec.Body.Bytes())
		}
		// Result().Header is what went out with the status line; a header
		// set after the body would show in rec.Header() but never be sent.
		if ct := rec.Result().Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("value %d: content type %q", i, ct)
		}
	}
	rec := httptest.NewRecorder()
	writeRawData(rec, "")
	if want := encoderEnvelope(""); !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatalf("empty result: encoder %q, spliced %q", want, rec.Body.Bytes())
	}
}

type rawSourcingStub struct {
	prv1.SourcingServiceClient
	result string
}

func (s rawSourcingStub) InquiryWorkspace(context.Context, *prv1.InquiryWorkspaceRequest, ...grpc.CallOption) (*prv1.InquiryWorkspaceResponse, error) {
	return &prv1.InquiryWorkspaceResponse{ResultJson: s.result}, nil
}

type rawDailyPriceStub struct {
	prv1.DailyPriceServiceClient
	result string
}

func (s rawDailyPriceStub) Execute(context.Context, *prv1.DailyPriceServiceExecuteRequest, ...grpc.CallOption) (*prv1.DailyPriceServiceExecuteResponse, error) {
	return &prv1.DailyPriceServiceExecuteResponse{ResultJson: s.result}, nil
}

// The raw result here has unescaped HTML characters, which json.Marshal
// would never write: a handler that went back to json.Encoder would escape
// them, so this also fails if a handler stops going through writeRawData.
func TestServiceResultsReachTheBrowserUntouched(t *testing.T) {
	raw := `{"items":[{"id":"7","body":{"title":"<Q235> & co"}}],"total":1}`
	for _, tc := range []struct{ raw, want string }{
		{raw, `{"success":true,"data":` + raw + "}\n"},
		{"", `{"success":true}` + "\n"},
	} {
		s := &Server{Sourcing: rawSourcingStub{result: tc.raw}, DailyPrices: rawDailyPriceStub{result: tc.raw}}
		for name, call := range map[string]func(http.ResponseWriter){
			"inquiry workspace": func(w http.ResponseWriter) {
				s.inquiryWorkspace(w, httptest.NewRequest(http.MethodPost, "/api/inquiry-workspace", strings.NewReader(`{"action":"list"}`)))
			},
			"daily prices": func(w http.ResponseWriter) {
				s.dailyPriceCall(w, httptest.NewRequest(http.MethodGet, "/api/daily-prices", nil), map[string]any{"action": "list"})
			},
		} {
			rec := httptest.NewRecorder()
			call(rec)
			res := rec.Result()
			if res.StatusCode != http.StatusOK || rec.Body.String() != tc.want || res.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("%s: %d %q %q", name, res.StatusCode, res.Header.Get("Content-Type"), rec.Body.String())
			}
		}
	}
}

// go test -run '^$' -bench RawData ./internal/httpapi — a list the size of a
// 70-inquiry company's, written both ways.
func BenchmarkRawData(b *testing.B) {
	type item struct {
		ID   string         `json:"id"`
		Body map[string]any `json:"body"`
	}
	products := make([]map[string]string, 9)
	for i := range products {
		products[i] = map[string]string{"product": "Hot rolled steel coil", "specification": "ASTM A36 · SS400 · 2.0mm · 1250mm", "quantity": "120", "unit": "MT", "remark": strings.Repeat("mill test certificate required ", 4)}
	}
	items := make([]item, 70)
	for i := range items {
		items[i] = item{ID: fmt.Sprint(i), Body: map[string]any{"title": "inquiry", "products": products, "template": strings.Repeat("field ", 350)}}
	}
	raw, _ := json.Marshal(map[string]any{"items": items, "total": len(items)})
	b.Logf("payload %d KB", len(raw)/1024)
	b.Run("encoder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = json.NewEncoder(httptest.NewRecorder()).Encode(envelope{Success: true, Data: json.RawMessage(raw)})
		}
	})
	b.Run("spliced", func(b *testing.B) {
		s := string(raw)
		for i := 0; i < b.N; i++ {
			writeRawData(httptest.NewRecorder(), s)
		}
	})
}
