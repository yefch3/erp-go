package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/app"
)

// 一次成功的回答：一行明细，够 Extract 走完全程。
func okBody() []byte {
	items, _ := json.Marshal(map[string]any{
		"title": "t", "summary": "s",
		"items": []map[string]string{{"product": "HRC", "quantity": "10", "quantity_unit": "MT"}},
	})
	b, _ := json.Marshal(map[string]any{
		"model": "gpt-5.6-luna", "status": "completed",
		"output": []any{map[string]any{"type": "message", "content": []any{
			map[string]any{"type": "output_text", "text": string(items)},
		}}},
	})
	return b
}

type reply struct {
	status     int
	body       string
	retryAfter string
	err        error
}

// scripted 按顺序回这几个答复，记下被请求了几次、每次等了多久。
func scripted(t *testing.T, replies ...reply) (*TableExtractor, *int, *[]time.Duration) {
	t.Helper()
	calls := 0
	var waits []time.Duration
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls >= len(replies) {
			t.Fatalf("asked %d times, only %d replies scripted", calls+1, len(replies))
		}
		rp := replies[calls]
		calls++
		if rp.err != nil {
			return nil, rp.err
		}
		body := rp.body
		if rp.status == http.StatusOK {
			body = string(okBody())
		}
		h := http.Header{"Content-Type": []string{"application/json"}}
		if rp.retryAfter != "" {
			h.Set("Retry-After", rp.retryAfter)
		}
		return &http.Response{StatusCode: rp.status, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	c := NewTableExtractor("test-key", "https://api.test/v1", "gpt-5.6-luna", time.Second).WithHTTPClient(hc)
	c.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	return c, &calls, &waits
}

func extract(c *TableExtractor) error {
	_, err := c.Extract(context.Background(), app.TableExtractionInput{Text: "询价"})
	return err
}

// 限流一下，等对方说的那几秒再发，第二次就成了——人看到的是一次成功的转换，
// 而不是「智能转换失败，请稍后重试」。
func TestARateLimitedRequestIsSentAgainAfterTheWaitItAskedFor(t *testing.T) {
	c, calls, waits := scripted(t,
		reply{status: 429, body: `{"error":{"message":"Rate limit reached","code":"rate_limit_exceeded"}}`, retryAfter: "3"},
		reply{status: 200},
	)
	if err := extract(c); err != nil {
		t.Fatalf("should have succeeded on the second try: %v", err)
	}
	if *calls != 2 || len(*waits) != 1 || (*waits)[0] != 3*time.Second {
		t.Errorf("calls %d, waits %v; want 2 calls and one 3s wait", *calls, *waits)
	}
}

// 对方一直出错：发满三次就停，归为「这会儿太忙」。
func TestAServerThatKeepsFailingIsTriedThreeTimesThenReportedBusy(t *testing.T) {
	c, calls, waits := scripted(t,
		reply{status: 503, body: `{"error":{"message":"overloaded"}}`},
		reply{status: 502, body: `bad gateway`},
		reply{status: 500, body: `{"error":{"message":"server error"}}`},
	)
	err := extract(c)
	if !errors.Is(err, app.ErrModelBusy) {
		t.Fatalf("want ErrModelBusy, got %v", err)
	}
	if *calls != maxAttempts || len(*waits) != maxAttempts-1 {
		t.Errorf("calls %d, waits %v", *calls, *waits)
	}
	if (*waits)[0] != 2*time.Second || (*waits)[1] != 6*time.Second {
		t.Errorf("waits without Retry-After should be 2s then 6s, got %v", *waits)
	}
}

// key 被拒、余额用完：重试没用，一次就停，归为账号问题——上面据此发专门的告警。
func TestARefusedKeyOrEmptyBalanceIsNotRetried(t *testing.T) {
	for name, rp := range map[string]reply{
		"invalid key":  {status: 401, body: `{"error":{"message":"Incorrect API key provided","code":"invalid_api_key"}}`},
		"forbidden":    {status: 403, body: `{"error":{"message":"not allowed"}}`},
		"out of money": {status: 429, body: `{"error":{"message":"You exceeded your current quota","code":"insufficient_quota"}}`},
	} {
		c, calls, _ := scripted(t, rp)
		err := extract(c)
		if !errors.Is(err, app.ErrModelAccount) {
			t.Errorf("%s: want ErrModelAccount, got %v", name, err)
		}
		if *calls != 1 {
			t.Errorf("%s: asked %d times, want 1", name, *calls)
		}
	}
}

// 请求本身有问题（比如文件类型不收）：再发一遍结果一样，不重试，也不算账号问题。
func TestABadRequestIsNotRetried(t *testing.T) {
	c, calls, _ := scripted(t, reply{status: 400, body: `{"error":{"message":"Invalid file data"}}`})
	err := extract(c)
	if err == nil || errors.Is(err, app.ErrModelBusy) || errors.Is(err, app.ErrModelAccount) {
		t.Fatalf("want a plain error, got %v", err)
	}
	if *calls != 1 {
		t.Errorf("asked %d times, want 1", *calls)
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// 超时不重试：两分钟没答完的，再发一遍多半还是两分钟。断开连接则再试。
func TestATimeoutIsNotRetriedButADroppedConnectionIs(t *testing.T) {
	c, calls, _ := scripted(t, reply{err: timeoutErr{}})
	if err := extract(c); err == nil || *calls != 1 {
		t.Errorf("timeout: err %v, calls %d; want an error after 1 call", err, *calls)
	}
	c, calls, _ = scripted(t, reply{err: errors.New("connection reset by peer")}, reply{status: 200})
	if err := extract(c); err != nil || *calls != 2 {
		t.Errorf("reset: err %v, calls %d; want success on the 2nd call", err, *calls)
	}
}

// 人走了（请求被取消）就别再等着重试。
func TestWaitingStopsWhenTheCallerGivesUp(t *testing.T) {
	c, calls, _ := scripted(t,
		reply{status: 429, body: `{"error":{"message":"slow down"}}`},
		reply{status: 200},
	)
	c.sleep = func(context.Context, time.Duration) error { return context.Canceled }
	if err := extract(c); !errors.Is(err, context.Canceled) || *calls != 1 {
		t.Errorf("err %v, calls %d; want context.Canceled after 1 call", err, *calls)
	}
}

func TestRetryAfterIsCapped(t *testing.T) {
	if d := retryDelay(1, "600"); d != maxRetryWait {
		t.Errorf("Retry-After 600 gave %v, want the cap %v", d, maxRetryWait)
	}
	if d := retryDelay(1, "soon"); d != 2*time.Second {
		t.Errorf("unreadable Retry-After gave %v, want the default 2s", d)
	}
}

// 2026-09-22 真失败过的那一次：附件类型写的是 application/octet-stream，原样交过去
// 被拒（HTTP 400 unsupported MIME type）。笼统的时候先看文件头，再看扩展名。
func TestAVagueFileTypeIsWorkedOutFromTheFileItself(t *testing.T) {
	pdf := []byte("%PDF-1.7\n...")
	for _, c := range []struct {
		name, ct string
		data     []byte
		want     string
	}{
		{"quote.bin", "application/octet-stream", pdf, "application/pdf"},
		{"RFQ.PDF", "", []byte("not really"), "application/pdf"},
		{"RFQ.docx", "application/octet-stream", []byte("PK\x03\x04"), "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"list", "application/octet-stream", []byte("HRC 1.2x1250 60 MT\n"), "text/plain"},
		{"scan.pdf", "application/pdf; name=scan.pdf", pdf, "application/pdf"},
		{"sheet.xlsx", "application/vnd.ms-excel", []byte("PK"), "application/vnd.ms-excel"},
		{"blob", "application/octet-stream", []byte{0x00, 0x01, 0x02}, "application/octet-stream"},
	} {
		if got := fileMediaType(c.name, c.ct, c.data); got != c.want {
			t.Errorf("%s (%q): got %s, want %s", c.name, c.ct, got, c.want)
		}
	}
}

// 走完整个 Extract：发出去的 file_data 带的是认出来的类型。
func TestTheFileSentToTheModelCarriesTheWorkedOutType(t *testing.T) {
	var sent []byte
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		sent, _ = io.ReadAll(r.Body)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(bytes.NewReader(okBody())), Request: r}, nil
	})}
	c := NewTableExtractor("test-key", "https://api.test/v1", "gpt-5.6-luna", time.Second).WithHTTPClient(hc)
	_, _ = c.Extract(context.Background(), app.TableExtractionInput{
		FileName: "询价单.pdf", ContentType: "application/octet-stream", FileData: []byte("%PDF-1.4 ..."),
	})
	if !bytes.Contains(sent, []byte(`data:application/pdf;base64,`)) || bytes.Contains(sent, []byte(`data:application/octet-stream`)) {
		t.Errorf("the file went out with the wrong type: %.300s", sent)
	}
}
