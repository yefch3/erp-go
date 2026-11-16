package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/blobstore"
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
	return mustRelay(blobstore.Config{
		Endpoint: strings.TrimPrefix(store.URL, "http://"), PublicEndpoint: "localhost:19000", Bucket: "erp-files",
	})
}

func mustRelay(cfg blobstore.Config) *StorageRelay {
	sr, err := NewStorageRelay(cfg)
	if err != nil {
		panic(err)
	}
	return sr
}

// 生产的存储配置，照 deploy/.env 的形状抄的（端点、区域、桶名的写法），密钥是
// 编的。测试里凡是要「生产地址」的地方，都拿它交给真的签名代码去签——不手写。
// 手写的那个版本就是 2026-11-09 上线即挂的原因：它少了一段 dualstack。
var productionStorage = blobstore.Config{
	Endpoint: "s3.us-west-2.amazonaws.com", PublicEndpoint: "s3.us-west-2.amazonaws.com",
	AccessKey: "AKIAEXAMPLE", SecretKey: "example", UseSSL: true,
	Bucket: "erp-files-564623062079", Region: "us-west-2",
}

// signedByTheStore 是业务服务签地址的四条路，一条不落。
func signedByTheStore(t *testing.T, cfg blobstore.Config, key string) map[string]string {
	t.Helper()
	store, err := blobstore.NewOffline(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	out := map[string]string{}
	for name, sign := range map[string]func() (string, error){
		"PresignedPut": func() (string, error) {
			u, err := store.PresignedPut(ctx, key, 10*time.Minute)
			return urlString(u), err
		},
		"PresignedGet": func() (string, error) {
			u, err := store.PresignedGet(ctx, key, 10*time.Minute)
			return urlString(u), err
		},
		"PresignedGetAs": func() (string, error) {
			u, err := store.PresignedGetAs(ctx, key, "发票.xlsx", 10*time.Minute)
			return urlString(u), err
		},
		"PresignedGetInline": func() (string, error) {
			u, err := store.PresignedGetInline(ctx, key, "application/pdf", 10*time.Minute)
			return urlString(u), err
		},
	} {
		raw, err := sign()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out[name] = raw
	}
	return out
}

func urlString(u interface{ String() string }) string {
	if u == nil {
		return ""
	}
	return u.String()
}

// 这一条就是生产上那次故障的回放：按生产配置签出来的地址，中转必须认，而且
// 原样去连（生产上对内对外是同一台）。第一版在这里答的是「不是本系统的文件存储」。
func TestStorageRelayAcceptsWhatTheProductionStoreSigns(t *testing.T) {
	relay := mustRelay(productionStorage)
	for name, raw := range signedByTheStore(t, productionStorage, "travel-reimbursements/4/4/1762-发票 (1).xlsx") {
		dial, host, err := relay.target(raw)
		if err != nil {
			t.Fatalf("%s: the relay refused an address its own store signed: %v\n%s", name, err, raw)
		}
		if dial.String() != raw {
			t.Fatalf("%s: production address rewritten on the way to the store:\n got %s\nwant %s", name, dial, raw)
		}
		if !strings.HasSuffix(host, ".amazonaws.com") || !strings.HasPrefix(host, productionStorage.Bucket+".") {
			t.Fatalf("%s: Host header %q", name, host)
		}
	}
}

// 开发环境同理：签的是 localhost:19000，连的是 minio:9000，Host 头不变。
func TestStorageRelayAcceptsWhatTheDevStoreSigns(t *testing.T) {
	cfg := blobstore.Config{Endpoint: "minio:9000", PublicEndpoint: "localhost:19000", AccessKey: "erp", SecretKey: "erp_dev_password", Bucket: "erp-files"}
	relay := mustRelay(cfg)
	for name, raw := range signedByTheStore(t, cfg, "quality/4/1/1762-质检 照片 (1).jpg") {
		dial, host, err := relay.target(raw)
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, raw)
		}
		if host != "localhost:19000" || dial.Host != "minio:9000" {
			t.Fatalf("%s: dial %s host %s", name, dial.Host, host)
		}
		if want := strings.Replace(raw, "localhost:19000", "minio:9000", 1); dial.String() != want {
			t.Fatalf("%s: path or query changed on the way to the store:\n got %s\nwant %s", name, dial, want)
		}
	}
}

// 区域是 AWS 签名主机名的一部分。网关少配一个 MINIO_REGION，认的就是别的区域的
// 地址，真的反而全拒——compose 里那一行因此不能删，这条测试替它站岗。
func TestStorageRelayNeedsTheSameRegionAsTheSigner(t *testing.T) {
	forgetful := productionStorage
	forgetful.Region = ""
	relay := mustRelay(forgetful)
	for name, raw := range signedByTheStore(t, productionStorage, "quality/4/1/a.jpg") {
		if _, _, err := relay.target(raw); err == nil {
			t.Fatalf("%s: a relay without the region accepted an address signed with it; the region no longer matters and this test can go", name)
		}
	}
}

func TestStorageRelayIsNotBuiltFromAConfigurationThatCannotSign(t *testing.T) {
	if _, err := NewStorageRelay(blobstore.Config{Endpoint: "localhost:19000"}); err == nil {
		t.Fatal("a relay was built without a bucket")
	}
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

// 别的桶、别的区域、别的写法——只要不是签名库会签出来的那一种，就不认。
// 这里的「错地址」可以手写：它们是反例，写错了最多是多拒一个。
func TestStorageRelayRefusesProductionLookalikes(t *testing.T) {
	relay := mustRelay(productionStorage)
	signed := "?X-Amz-Credential=x&X-Amz-Signature=y"
	for name, raw := range map[string]string{
		"another bucket, same store":         "https://other-bucket.s3.dualstack.us-west-2.amazonaws.com/x.pdf" + signed,
		"our bucket, another region":         "https://erp-files-564623062079.s3.dualstack.us-east-1.amazonaws.com/x.pdf" + signed,
		"the name the first version assumed": "https://erp-files-564623062079.s3.us-west-2.amazonaws.com/x.pdf" + signed,
		"path-style on the endpoint":         "https://s3.us-west-2.amazonaws.com/erp-files-564623062079/x.pdf" + signed,
		"our host as a prefix":               "https://erp-files-564623062079.s3.dualstack.us-west-2.amazonaws.com.evil.example/x.pdf" + signed,
		"plain http":                         "http://erp-files-564623062079.s3.dualstack.us-west-2.amazonaws.com/x.pdf" + signed,
		"no key":                             "https://erp-files-564623062079.s3.dualstack.us-west-2.amazonaws.com/" + signed,
		// U+212A KELVIN SIGN：Unicode 大小写折叠下它等于 k，但它不是 k。
		"a Kelvin sign for the k": "https://erp-files-564623062079.s3.dualstac\u212A.us-west-2.amazonaws.com/x.pdf" + signed,
	} {
		if _, _, err := relay.target(raw); err == nil {
			t.Fatalf("%s was accepted: %s", name, raw)
		}
	}
}

// 拒绝一个地址时，日志里要有「收到哪台、本该是哪台」，而且不能有签名。
// 第一版没记这一行：生产上认错主机两天，日志里只有一串 400。
func TestStorageRelayLogsWhichHostItRefused(t *testing.T) {
	var logged bytes.Buffer
	s := &Server{Storage: mustRelay(productionStorage), Log: slog.New(slog.NewTextHandler(&logged, nil))}
	raw := "https://erp-files-564623062079.s3.us-west-2.amazonaws.com/x.pdf?X-Amz-Credential=cred123&X-Amz-Signature=sig456"

	w := httptest.NewRecorder()
	s.relayUpload(w, relayRequest(http.MethodPut, raw, bytes.NewReader([]byte("x")), ""))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	for _, want := range []string{
		"storage relay refused an address",
		"offered_host=erp-files-564623062079.s3.us-west-2.amazonaws.com",
		"signed_host=erp-files-564623062079.s3.dualstack.us-west-2.amazonaws.com",
	} {
		if !strings.Contains(logged.String(), want) {
			t.Fatalf("log is missing %q:\n%s", want, logged.String())
		}
	}
	for _, secret := range []string{"cred123", "sig456", "X-Amz-Signature"} {
		if strings.Contains(logged.String(), secret) {
			t.Fatalf("%s leaked into the log:\n%s", secret, logged.String())
		}
	}
}

func TestStorageRelayLogAttrsNameBothHosts(t *testing.T) {
	dev := mustRelay(blobstore.Config{Endpoint: "minio:9000", PublicEndpoint: "localhost:19000", Bucket: "erp-files"})
	got := fmt.Sprint(dev.LogAttrs()...)
	for _, want := range []string{"localhost:19000", "minio:9000", "/erp-files/"} {
		if !strings.Contains(got, want) {
			t.Fatalf("LogAttrs %q is missing %q", got, want)
		}
	}
	prod := fmt.Sprint(mustRelay(productionStorage).LogAttrs()...)
	if strings.Count(prod, "erp-files-564623062079.s3.dualstack.us-west-2.amazonaws.com") != 2 {
		t.Fatalf("production signs and dials the same host; LogAttrs says %q", prod)
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

	unreachable := &Server{Storage: mustRelay(blobstore.Config{Endpoint: "127.0.0.1:1", PublicEndpoint: "localhost:19000", Bucket: "erp-files"}), Log: logger}
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
	s := &Server{Storage: mustRelay(blobstore.Config{Endpoint: "127.0.0.1:1", PublicEndpoint: "localhost:19000", Bucket: "erp-files"})}

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

// 走真的路由，而不是直接调处理函数：登录、CSRF、防重、权限缓存这几层中间件
// 都在前面，任何一层去读请求体、给它设上限或者改了方法匹配，直接调函数的测试
// 都看不见。5 MB 比任何一个 JSON 接口的上限都大，过得去才说明这条路没被谁
// 顺手卡住。
func TestStorageRelayThroughTheRealRouter(t *testing.T) {
	store, calls := fakeStore(t, http.StatusOK, map[string]string{"Content-Type": "image/jpeg"}, "stored bytes")
	s := renewServer(t, time.Hour, nil)
	s.Storage = relayFor(store)
	router := s.Router()
	token := tokenAged(t, time.Minute, 12*time.Hour, 42, time.Time{})
	payload := bytes.Repeat([]byte("0123456789abcdef"), 5<<20/16)

	request := func(method string, body io.Reader, session, csrf bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/storage/relay", body)
		req.Header.Set(StorageURLHeader, signedPut)
		if session {
			req.AddCookie(&http.Cookie{Name: SessionCookie, Value: token})
			req.AddCookie(&http.Cookie{Name: CSRFCookie, Value: "echo"})
		}
		if csrf {
			req.Header.Set(CSRFHeader, "echo")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := request(http.MethodPut, bytes.NewReader(payload), false, false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("an upload without a session: status %d, want 401", rec.Code)
	}
	if rec := request(http.MethodPut, bytes.NewReader(payload), true, false); rec.Code != http.StatusForbidden {
		t.Fatalf("an upload without the CSRF echo: status %d, want 403", rec.Code)
	}
	if len(*calls) != 0 {
		t.Fatalf("a refused upload reached the store: %d calls", len(*calls))
	}

	rec := request(http.MethodPut, bytes.NewReader(payload), true, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload through the router: status %d: %s", rec.Code, rec.Body.String())
	}
	if len(*calls) != 1 || !bytes.Equal((*calls)[0].body, payload) {
		t.Fatalf("the store did not receive the 5 MB body intact: %d calls", len(*calls))
	}

	rec = request(http.MethodGet, nil, true, false)
	if rec.Code != http.StatusOK || rec.Body.String() != "stored bytes" {
		t.Fatalf("download through the router: status %d body %q", rec.Code, rec.Body.String())
	}
}
