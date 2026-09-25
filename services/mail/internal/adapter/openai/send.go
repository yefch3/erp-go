package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/app"
)

// 一次转换最多发几遍。第一遍加两次重试：限流一般几秒就过去，再多等只是让人
// 对着转圈干等——那时该让人知道「这会儿太忙」，而不是一直转。
const maxAttempts = 3

// Retry-After 最多听它等多久。对方要是说等一分钟，那已经不是「稍等一下」了。
const maxRetryWait = 30 * time.Second

// post 发一次请求，限流和对方临时出错时等一下再发。
//
// 哪些重试、哪些不重试：
//   - 429（限流）、500/502/503/504：对方一时忙不过来，等一下通常就好。
//   - 连接失败（断开、拒绝）：同上。
//   - **超时不重试**：一个 2 分钟都没答完的请求，再发一遍多半还是 2 分钟，
//     人要多等好几分钟才看到同样的失败。
//   - 401/403、429 且 code=insufficient_quota：key 被拒、余额用完。重试没用，
//     归为 app.ErrModelAccount，让上面发专门的告警。
//   - 其余 4xx：请求本身有问题（比如文件类型不收），重试结果一样。
//
// 重试几次仍是限流或出错，归为 app.ErrModelBusy。
func (c *TableExtractor) post(ctx context.Context, body []byte) (*http.Response, error) {
	for attempt := 1; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.client.Do(req)
		if err != nil {
			if ctx.Err() != nil || isTimeout(err) || attempt == maxAttempts {
				return nil, fmt.Errorf("OpenAI request: %w", err)
			}
			if err := c.pause(ctx, retryDelay(attempt, "")); err != nil {
				return nil, err
			}
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		message, code := apiError(data)
		failure := fmt.Errorf("OpenAI returned HTTP %d: %s", resp.StatusCode, message)
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden ||
			(resp.StatusCode == http.StatusTooManyRequests && code == "insufficient_quota"):
			return nil, fmt.Errorf("%w: %w", app.ErrModelAccount, failure)
		case retryableStatus(resp.StatusCode):
			if attempt == maxAttempts {
				return nil, fmt.Errorf("%w: %w", app.ErrModelBusy, failure)
			}
			if err := c.pause(ctx, retryDelay(attempt, resp.Header.Get("Retry-After"))); err != nil {
				return nil, err
			}
		default:
			return nil, failure
		}
	}
}

func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryDelay 是第 attempt 次失败之后等多久。对方说了等几秒就听它的（有上限），
// 没说就 2 秒、6 秒。
func retryDelay(attempt int, retryAfter string) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && secs >= 0 {
		return min(time.Duration(secs)*time.Second, maxRetryWait)
	}
	if attempt <= 1 {
		return 2 * time.Second
	}
	return 6 * time.Second
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
}

// pause 等 d，人走了（ctx 取消）就不等了。测试里换成不等的版本。
func (c *TableExtractor) pause(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// apiError 读出对方错误回包里的说明和错误码。
func apiError(data []byte) (message, code string) {
	var v struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &v) == nil && v.Error.Message != "" {
		return v.Error.Message, v.Error.Code
	}
	text := strings.TrimSpace(string(data))
	if len(text) > 500 {
		text = text[:500]
	}
	if text == "" {
		text = "empty error response"
	}
	return text, ""
}

// vagueMediaTypes 是邮件里常见的「没说是什么」的类型。
var vagueMediaTypes = map[string]bool{
	"": true, "application/octet-stream": true, "binary/octet-stream": true,
	"application/unknown": true, "application/download": true,
	"application/x-download": true, "application/force-download": true,
}

// mediaTypeByExt 是笼统类型时按扩展名认的那几种——和 app 里 supportedTableSource
// 放行的扩展名对齐（图片走 imageMediaType，不在这里）。
var mediaTypeByExt = map[string]string{
	".pdf":  "application/pdf",
	".txt":  "text/plain",
	".csv":  "text/csv",
	".tsv":  "text/tab-separated-values",
	".md":   "text/markdown",
	".rtf":  "application/rtf",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".odt":  "application/vnd.oasis.opendocument.text",
	".ppt":  "application/vnd.ms-powerpoint",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ods":  "application/vnd.oasis.opendocument.spreadsheet",
}

// fileMediaType 是交给模型厂的文件类型。
//
// 邮件里的附件常把类型写得很笼统（application/octet-stream 之类），原样交过去会
// 被拒：2026-09-22 真发生过一次，HTTP 400「unsupported MIME type
// 'application/octet-stream'」，那次转换就失败了。所以笼统的时候先看文件头
// （PDF 开头就是 %PDF-），再看扩展名，最后看字节像不像纯文本；都认不出就原样
// 交过去，让对方把为什么不收说清楚。写得具体的类型不动。
func fileMediaType(name, contentType string, data []byte) string {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if !vagueMediaTypes[ct] {
		return ct
	}
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		return "application/pdf"
	}
	if t, ok := mediaTypeByExt[strings.ToLower(extOf(name))]; ok {
		return t
	}
	if detected := detectMediaType(data); !vagueMediaTypes[detected] && strings.HasPrefix(detected, "text/") {
		return detected
	}
	if ct == "" {
		return "application/octet-stream"
	}
	return ct
}

func extOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}
