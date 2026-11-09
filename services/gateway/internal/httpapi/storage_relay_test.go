package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 这组测试里的「存储」是一个假的 S3：记下到它那儿的每个请求，按吩咐应答。
// 中转的全部职责就是把浏览器的请求原样送到这儿、把这儿的回答原样带回去，
// 所以断言的都是「到达存储的东西是不是和签名时一模一样」。

type storeCall struct {
	method, host, requestURI, path, contentType string
	body                                        []byte
}

func fakeStore(t *testing.T, status int, headers map[string]string, body string) (*httptest.Server, *[]storeCall) {
	t.Helper()
	calls := &[]storeCall{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*calls = append(*calls, storeCall{r.Method, r.Host, r.RequestURI, r.URL.Path, r.Header.Get("Content-Type"), b})
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, calls
}

// 开发环境的形状：浏览器拿到的地址写着 localhost:19000，网关实际要连的是另一个
// 主机（这里是假存储的地址）。签名盖住了 Host，所以连谁可以变、Host 不能变。
func relayFor(store *httptest.Server) *StorageRelay {
	return NewStorageRelay(strings.TrimPrefix(store.URL, "http://"), "localhost:19000", "erp-files", false)
}

// 一个 minio-go 签出来的直传地址的样子：路径式、中文文件名已百分号编码。
const signedPut = "http://localhost:19000/erp-files/quality/4/1/1762-%E7%85%A7%E7%89%87.jpg" +
	"?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=erp%2F20261109%2Fus-east-1%2Fs3%2Faws4_request" +
	"&X-Amz-Date=20261109T012740Z&X-Amz-Expires=600&X-Amz-SignedHeaders=host&X-Amz-Signature=abc123"

func relayRequest(method, storageURL string, body io.Reader, contentType string) *http.Request {
	r := httptest.NewRequest(method, "/api/storage/relay", body)
	if storageURL != "" {
		r.Header.Set(StorageURLHeader, storageURL)
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return r
}

func decodeEnvelope(t *testing.T, w *httptest.ResponseRecorder) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("response is not an envelope: %v\n%s", err, w.Body.String())
	}
	return env
}

func TestStorageRelayUploadReachesTheStoreExactlyAsSigned(t *testing.T) {
	store, calls := fakeStore(t, http.StatusOK, map[string]string{"ETag": `"d41d8cd9"`}, "")
	s := &Server{Storage: relayFor(store)}

	w := httptest.NewRecorder()
	s.relayUpload(w, relayRequest(http.MethodPut, signedPut, bytes.NewReader([]byte("jpeg bytes")), "image/jpeg"))

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	env := decodeEnvelope(t, w)
	if !env.Success || !strings.Contains(string(env.Data), `"etag":"d41d8cd9"`) {
		t.Fatalf("unexpected envelope: %s", w.Body.String())
	}
	if len(*calls) != 1 {
		t.Fatalf("store saw %d requests, want 1", len(*calls))
	}
	got := (*calls)[0]
	if got.method != http.MethodPut {
		t.Fatalf("store saw %s, want PUT", got.method)
	}
	// 签名盖住的 Host：连的是假存储，报的仍是地址里写的那个。
	if got.host != "localhost:19000" {
		t.Fatalf("Host header %q, want the signed host localhost:19000", got.host)
	}
	// 路径和查询串一个字节都不能动——签名就是按这串算的。
	wantURI := strings.TrimPrefix(signedPut, "http://localhost:19000")
	if got.requestURI != wantURI {
		t.Fatalf("request URI changed on the way through:\n got %s\nwant %s", got.requestURI, wantURI)
	}
	if got.contentType != "image/jpeg" {
		t.Fatalf("Content-Type %q not passed through", got.contentType)
	}
	if string(got.body) != "jpeg bytes" {
		t.Fatalf("body %q not passed through", got.body)
	}
}

func TestStorageRelayUploadDefaultsContentType(t *testing.T) {
	store, calls := fakeStore(t, http.StatusOK, nil, "")
	s := &Server{Storage: relayFor(store)}

	w := httptest.NewRecorder()
	s.relayUpload(w, relayRequest(http.MethodPut, signedPut, bytes.NewReader([]byte("x")), ""))

	if w.Code != http.StatusOK || len(*calls) != 1 {
		t.Fatalf("status %d, calls %d: %s", w.Code, len(*calls), w.Body.String())
	}
	if (*calls)[0].contentType != "application/octet-stream" {
		t.Fatalf("Content-Type %q, want application/octet-stream", (*calls)[0].contentType)
	}
}

func TestStorageRelayRefusesAddressesOutsideOurStore(t *testing.T) {
	store, calls := fakeStore(t, http.StatusOK, nil, "")
	s := &Server{Storage: relayFor(store)}
	signed := "?X-Amz-Credential=erp%2Fx&X-Amz-Signature=abc"
	for name, raw := range map[string]string{
		"foreign host":     "http://evil.example/erp-files/quality/4/1/a.jpg" + signed,
		"other bucket":     "http://localhost:19000/other-bucket/quality/4/1/a.jpg" + signed,
		"unsigned":         "http://localhost:19000/erp-files/quality/4/1/a.jpg",
		"wrong scheme":     "https://localhost:19000/erp-files/quality/4/1/a.jpg" + signed,
		"userinfo":         "http://erp@localhost:19000/erp-files/quality/4/1/a.jpg" + signed,
		"bucket root only": "http://localhost:19000/erp-files/" + signed,
		"missing":          "",
		"garbage":          "not a url at all",
		// 桶名对、路径却往上爬；解码前后两种写法。
		"traversal":         "http://localhost:19000/erp-files/../private/x.jpg" + signed,
		"encoded traversal": "http://localhost:19000/erp-files/%2e%2e/private/x.jpg" + signed,
		"double slash":      "http://localhost:19000/erp-files//x.jpg" + signed,
		// 像我们的主机、不是我们的主机。
		"lookalike host":    "http://erp-files.localhost.evil.example:19000/x.jpg" + signed,
		"trailing dot host": "http://localhost.:19000/erp-files/x.jpg" + signed,
	} {
		for _, method := range []string{http.MethodPut, http.MethodGet} {
			w := httptest.NewRecorder()
			var body io.Reader
			if method == http.MethodPut {
				body = bytes.NewReader([]byte("x"))
			}
			r := relayRequest(method, raw, body, "")
			if method == http.MethodPut {
				s.relayUpload(w, r)
			} else {
				s.relayDownload(w, r)
			}
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%s %s: status %d, want 400: %s", method, name, w.Code, w.Body.String())
			}
			if env := decodeEnvelope(t, w); env.Code != "STORAGE_RELAY_URL_REJECTED" {
				t.Fatalf("%s %s: code %q", method, name, env.Code)
			}
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("a rejected address still reached the store: %+v", *calls)
	}
}

func TestStorageRelayTargetKeepsTheSignedHostWhileDialingTheInternalOne(t *testing.T) {
	// 开发环境：对外 localhost:19000，对内 minio:9000。
	dev := NewStorageRelay("minio:9000", "localhost:19000", "erp-files", false)
	dial, host, err := dev.target(signedPut)
	if err != nil {
		t.Fatal(err)
	}
	if dial.Host != "minio:9000" || host != "localhost:19000" {
		t.Fatalf("dial %s host %s", dial.Host, host)
	}
	if !strings.Contains(dial.String(), "/1762-%E7%85%A7%E7%89%87.jpg?X-Amz-Algorithm=") {
		t.Fatalf("encoded path or query altered: %s", dial.String())
	}
	// 虚拟主机式的桶名前缀跟着一起换。
	dial, host, err = dev.target("http://erp-files.localhost:19000/quality/4/1/a.jpg?X-Amz-Credential=x&X-Amz-Signature=y")
	if err != nil {
		t.Fatal(err)
	}
	if dial.Host != "erp-files.minio:9000" || host != "erp-files.localhost:19000" {
		t.Fatalf("virtual-hosted dial %s host %s", dial.Host, host)
	}

	// 生产：对内对外同一个地址，什么都不改写。AWS 对没有点的桶名用虚拟主机式。
	prod := NewStorageRelay("s3.us-west-2.amazonaws.com", "s3.us-west-2.amazonaws.com", "erp-files-564623062079", true)
	raw := "https://erp-files-564623062079.s3.us-west-2.amazonaws.com/travel-reimbursements/4/2/1762-a.pdf?X-Amz-Credential=x&X-Amz-Signature=y"
	dial, host, err = prod.target(raw)
	if err != nil {
		t.Fatal(err)
	}
	if dial.String() != raw || host != "erp-files-564623062079.s3.us-west-2.amazonaws.com" {
		t.Fatalf("production address rewritten: %s (host %s)", dial, host)
	}
	if _, _, err := prod.target("https://other-bucket.s3.us-west-2.amazonaws.com/x?X-Amz-Credential=x&X-Amz-Signature=y"); err == nil {
		t.Fatal("another bucket on the same AWS host was accepted")
	}
	if _, _, err := prod.target("https://s3.us-west-2.amazonaws.com/erp-files-564623062079/x?X-Amz-Credential=x&X-Amz-Signature=y"); err != nil {
		t.Fatalf("path-style address for our own bucket rejected: %v", err)
	}
}

func TestStorageRelayUploadReportsStoreRefusal(t *testing.T) {
	store, _ := fakeStore(t, http.StatusForbidden, nil, "<Error><Code>SignatureDoesNotMatch</Code></Error>")
	s := &Server{Storage: relayFor(store)}

	w := httptest.NewRecorder()
	s.relayUpload(w, relayRequest(http.MethodPut, signedPut, bytes.NewReader([]byte("x")), "image/jpeg"))

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	env := decodeEnvelope(t, w)
	// 状态码之外把存储自己的说法也带上：「签名过期」和「密钥不对」才分得开。
	if env.Code != "STORAGE_RELAY_REJECTED" || !strings.Contains(env.Message, "403") || !strings.Contains(env.Message, "SignatureDoesNotMatch") {
		t.Fatalf("envelope %+v", env)
	}
}

// 日志里永远不能出现签名：*url.Error 的文本带着完整地址，直接记它就泄露了。
func TestStorageRelayLogsNeverCarryTheSignature(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))

	unreachable := &Server{Storage: NewStorageRelay("127.0.0.1:1", "localhost:19000", "erp-files", false), Log: logger}
	w := httptest.NewRecorder()
	unreachable.relayUpload(w, relayRequest(http.MethodPut, signedPut, bytes.NewReader([]byte("x")), ""))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(logged.String(), "storage relay upload failed") || !strings.Contains(logged.String(), "connection refused") {
		t.Fatalf("the failure was not logged usefully: %s", logged.String())
	}

	store, _ := fakeStore(t, http.StatusForbidden, nil, "<Error><Code>SignatureDoesNotMatch</Code></Error>")
	refused := &Server{Storage: relayFor(store), Log: logger}
	w = httptest.NewRecorder()
	refused.relayUpload(w, relayRequest(http.MethodPut, signedPut, bytes.NewReader([]byte("x")), ""))
	if !strings.Contains(logged.String(), "code=SignatureDoesNotMatch") {
		t.Fatalf("the store's own reason was not logged: %s", logged.String())
	}

	for _, secret := range []string{"X-Amz-Signature", "abc123", "X-Amz-Credential"} {
		if strings.Contains(logged.String(), secret) {
			t.Fatalf("%s leaked into the log:\n%s", secret, logged.String())
		}
	}
}

// 浏览器自己断了：没人等答案，也不该记成存储的错。
func TestStorageRelayStaysQuietWhenTheBrowserHangsUp(t *testing.T) {
	var logged bytes.Buffer
	store, _ := fakeStore(t, http.StatusOK, nil, "")
	s := &Server{Storage: relayFor(store), Log: slog.New(slog.NewTextHandler(&logged, nil))}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := relayRequest(http.MethodPut, signedPut, bytes.NewReader([]byte("x")), "").WithContext(ctx)
	w := httptest.NewRecorder()
	s.relayUpload(w, r)

	if w.Body.Len() != 0 || logged.Len() != 0 {
		t.Fatalf("answered or logged after the client left: body %q log %q", w.Body.String(), logged.String())
	}
}

func TestStorageRelayUploadReportsUnreachableStore(t *testing.T) {
	// 127.0.0.1:1 没人听：连接立刻被拒，和「连不上」是同一类错。
	s := &Server{Storage: NewStorageRelay("127.0.0.1:1", "localhost:19000", "erp-files", false)}

	w := httptest.NewRecorder()
	s.relayUpload(w, relayRequest(http.MethodPut, signedPut, bytes.NewReader([]byte("x")), ""))

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if env := decodeEnvelope(t, w); env.Code != "STORAGE_RELAY_UNREACHABLE" {
		t.Fatalf("code %q", env.Code)
	}
}

// 没有长度就不能转发：S3 的签名 PUT 不接受分块传输。
type unsizedReader struct{ r io.Reader }

func (u unsizedReader) Read(p []byte) (int, error) { return u.r.Read(p) }

func TestStorageRelayUploadRefusesOversizedAndUnsizedBodies(t *testing.T) {
	store, calls := fakeStore(t, http.StatusOK, nil, "")
	s := &Server{Storage: relayFor(store)}

	w := httptest.NewRecorder()
	big := relayRequest(http.MethodPut, signedPut, bytes.NewReader([]byte("x")), "")
	big.ContentLength = storageRelayMaxBytes + 1
	s.relayUpload(w, big)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized: status %d: %s", w.Code, w.Body.String())
	}
	if env := decodeEnvelope(t, w); env.Code != "STORAGE_RELAY_TOO_LARGE" {
		t.Fatalf("oversized: code %q", env.Code)
	}

	w = httptest.NewRecorder()
	s.relayUpload(w, relayRequest(http.MethodPut, signedPut, unsizedReader{strings.NewReader("x")}, ""))
	if w.Code != http.StatusLengthRequired {
		t.Fatalf("unsized: status %d: %s", w.Code, w.Body.String())
	}
	if len(*calls) != 0 {
		t.Fatalf("a refused body still reached the store: %+v", *calls)
	}
}

func TestStorageRelayUploadAcceptsAnEmptyFileWithAnExplicitLength(t *testing.T) {
	store, calls := fakeStore(t, http.StatusOK, nil, "")
	s := &Server{Storage: relayFor(store)}

	w := httptest.NewRecorder()
	s.relayUpload(w, relayRequest(http.MethodPut, signedPut, bytes.NewReader(nil), ""))

	if w.Code != http.StatusOK || len(*calls) != 1 || len((*calls)[0].body) != 0 {
		t.Fatalf("status %d calls %d: %s", w.Code, len(*calls), w.Body.String())
	}
}

func TestStorageRelayDownloadHandsTheFileBackAsAnAttachment(t *testing.T) {
	store, calls := fakeStore(t, http.StatusOK, map[string]string{
		"Content-Type":        "application/pdf",
		"Content-Disposition": `inline; filename="a.pdf"`,
		"ETag":                `"abc"`,
	}, "%PDF-1.4 bytes")
	s := &Server{Storage: relayFor(store)}

	w := httptest.NewRecorder()
	s.relayDownload(w, relayRequest(http.MethodGet, signedPut, nil, ""))

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "%PDF-1.4 bytes" {
		t.Fatalf("body %q", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("Content-Type %q", ct)
	}
	// 从我们自己的域名吐出来的用户文件一律当下载，不让浏览器当页面渲染。
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") || !strings.Contains(cd, "a.pdf") {
		t.Fatalf("Content-Disposition %q", cd)
	}
	if w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("cache/sniff headers: %v", w.Header())
	}
	if len(*calls) != 1 || (*calls)[0].method != http.MethodGet || (*calls)[0].host != "localhost:19000" {
		t.Fatalf("store saw %+v", *calls)
	}
}

func TestStorageRelayDownloadMapsStoreAnswers(t *testing.T) {
	for _, tc := range []struct {
		store  int
		status int
		code   string
	}{
		{http.StatusNotFound, http.StatusNotFound, "STORAGE_RELAY_NOT_FOUND"},
		{http.StatusForbidden, http.StatusBadGateway, "STORAGE_RELAY_REJECTED"},
	} {
		store, _ := fakeStore(t, tc.store, nil, "<Error/>")
		s := &Server{Storage: relayFor(store)}
		w := httptest.NewRecorder()
		s.relayDownload(w, relayRequest(http.MethodGet, signedPut, nil, ""))
		if w.Code != tc.status {
			t.Fatalf("store %d: status %d: %s", tc.store, w.Code, w.Body.String())
		}
		if env := decodeEnvelope(t, w); env.Code != tc.code {
			t.Fatalf("store %d: code %q", tc.store, env.Code)
		}
	}
}

func TestStorageRelayAnswersUnavailableWhenNotConfigured(t *testing.T) {
	s := &Server{}
	for _, method := range []string{http.MethodPut, http.MethodGet} {
		w := httptest.NewRecorder()
		r := relayRequest(method, signedPut, bytes.NewReader([]byte("x")), "")
		if method == http.MethodPut {
			s.relayUpload(w, r)
		} else {
			s.relayDownload(w, r)
		}
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status %d", method, w.Code)
		}
		if env := decodeEnvelope(t, w); env.Code != "STORAGE_RELAY_UNAVAILABLE" {
			t.Fatalf("%s: code %q", method, env.Code)
		}
	}
}
