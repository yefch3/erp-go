// Package gotenberg turns office documents into PDFs by asking a LibreOffice
// running in its own container.
//
// 为什么是另一个容器，而不是本进程里调 soffice：转换要打开客户发来的、谁都
// 没检查过的文件，而 LibreOffice 是一个几百万行、历年出过远程执行漏洞的
// 桌面软件。把它关在一个只连内网、不出公网的容器里，是这件事该有的做法。
// 顺带还解决了 headless LibreOffice 那些烦人的老问题：并发要各自的 profile
// 目录、崩了留僵尸进程、卡死没有超时——Gotenberg 已经把这些管好了。
package gotenberg

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// Client 是一个 Gotenberg 的地址加一个超时。
type Client struct {
	baseURL string
	http    *http.Client
}

// New 返回一个客户端；地址为空返回 nil，调用方据此认为「没配转换」。
//
// 超时不是可选项：一份构造得当的文档能让 LibreOffice 一直忙下去，而这个
// 请求正挂在某个人的「预览」按钮上。
func New(baseURL string, timeout time.Duration) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: timeout}}
}

// ToPDF 把一份文档转成 PDF。
func (c *Client) ToPDF(ctx context.Context, fileName string, data []byte) ([]byte, error) {
	// 没配地址时 New 返回空指针，而空指针塞进接口字段会变成一个非空的接口
	// （Go 的老坑）。main.go 那边显式判过空了；这里再挡一层，是为了让接错线
	// 的后果是一句错误消息，而不是把邮件服务打崩。
	if c == nil || c.http == nil {
		return nil, fmt.Errorf("document converter is not configured")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("nothing to convert")
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	// 只留扩展名，而且是清洗过的。文件名来自邮件头，是发信人写的：带路径的
	// 名字（"../../x.docx"）不能原样送进另一个服务的表单。转换器需要的只有
	// 「这是个什么格式」，那就只给它这个。
	part, err := w.CreateFormFile("files", safeName(fileName))
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/forms/libreoffice/convert", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// 转换器把出错原因写在正文里，读一小段带回去——但只进日志，不给用户。
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("convert failed: %s: %s",
			resp.Status, strings.TrimSpace(string(msg)))
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		// 200 但不是 PDF：宁可报错，也不要把一坨不知道是什么的字节当成 PDF
		// 存进缓存——存进去之后每次预览都会拿到它。
		return nil, fmt.Errorf("convert returned %d bytes that are not a PDF", len(out))
	}
	return out, nil
}

// safeName 把发信人给的文件名压成一个安全的、只保留格式信息的名字。
//
// 扩展名是转换器挑过滤器的唯一依据，也是这里唯一值得保留的东西。路径分隔符、
// 转义序列、非 ASCII 一律不带过去。
func safeName(fileName string) string {
	ext := strings.ToLower(filepath.Ext(filepath.Base(strings.TrimSpace(fileName))))
	ext = strings.Map(func(r rune) rune {
		if r == '.' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, ext)
	if ext == "" || ext == "." {
		return "document"
	}
	return "document" + ext
}
