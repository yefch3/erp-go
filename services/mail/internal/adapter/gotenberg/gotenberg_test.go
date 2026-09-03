package gotenberg

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSenderSuppliedFileNameIsReducedToItsExtension(t *testing.T) {
	cases := map[string]string{
		"报价单.xlsx":             "document.xlsx",
		"quote.DOCX":           "document.docx",
		"../../etc/passwd.doc": "document.doc",
		`C:\Users\x\a.xls`:     "document.xls",
		"a\"b;c.ppt":           "document.ppt",
		"noextension":          "document",
		"":                     "document",
		"trailing.":            "document",
	}
	for in, want := range cases {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConvertPostsTheFileAndReturnsThePDF(t *testing.T) {
	var gotName string
	var gotBytes []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/forms/libreoffice/convert" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatal(err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		part, err := mr.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		gotName = part.FileName()
		gotBytes, _ = io.ReadAll(part)
		_, _ = w.Write([]byte("%PDF-1.4 ok"))
	}))
	defer srv.Close()

	c := New(srv.URL, 5*time.Second)
	out, err := c.ToPDF(context.Background(), "报价单.xlsx", []byte("source bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "%PDF-1.4 ok" {
		t.Errorf("got %q", out)
	}
	if gotName != "document.xlsx" {
		t.Errorf("转换器收到的文件名 %q，应该只剩扩展名", gotName)
	}
	if string(gotBytes) != "source bytes" {
		t.Errorf("送过去的内容 %q", gotBytes)
	}
}

// 200 但不是 PDF 的响应必须报错。存进缓存之后每次预览都会拿到它，
// 所以这里松一寸，用户就永远看不到那个附件了。
func TestANonPDFResponseIsRejectedEvenWith200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>Gateway is confused</html>"))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, time.Second).ToPDF(context.Background(), "a.docx", []byte("x")); err == nil {
		t.Fatal("不是 PDF 却当成功返回了")
	}
}

func TestConvertReportsTheServersReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("file format not supported"))
	}))
	defer srv.Close()
	_, err := New(srv.URL, time.Second).ToPDF(context.Background(), "a.docx", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("错误里应该带上转换器说的原因：%v", err)
	}
}

// 地址没配就没有转换器，让调用方能分辨「没配」和「配了但坏了」。
func TestNoAddressMeansNoConverter(t *testing.T) {
	for _, s := range []string{"", "   "} {
		if c := New(s, time.Second); c != nil {
			t.Errorf("New(%q) 应该返回 nil", s)
		}
	}
}

// 接错线的后果应该是一句错误，不是一次崩溃。见 New 返回空指针那一段。
func TestANilClientDoesNotPanic(t *testing.T) {
	var c *Client
	if _, err := c.ToPDF(context.Background(), "a.docx", []byte("x")); err == nil {
		t.Fatal("空的客户端应该报未配置")
	}
}
