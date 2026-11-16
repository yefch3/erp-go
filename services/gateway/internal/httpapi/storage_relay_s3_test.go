package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/blobstore"
)

// 对着真的 AWS S3 走到门口为止：按生产的存储配置签一个地址（密钥是编的），
// 交给中转，看 S3 **自己**怎么答。
//
// 要的答案是 S3 的 403 InvalidAccessKeyId——它说明三件事同时成立：中转认了这个
// 地址、连上了签名里那台主机（TLS 和 Host 头都对）、S3 读到了签名并且只对密钥
// 有意见。认错主机会停在中转（400，根本不出门）；区域不对是 301；Host 头不对
// 是别的桶或 400。真密钥的完整往返只有登录后的生产环境能做，这是不带密钥能
// 走到的最远处。
//
// 不进 CI（CI 不该依赖 AWS 在不在）。改了认地址的规则、升了签名库之后手动跑：
//
//	GATEWAY_TEST_REAL_S3=1 go test ./internal/httpapi/ -run RealS3 -count=1 -v
func TestStorageRelayReachesRealS3WithTheProductionShape(t *testing.T) {
	if os.Getenv("GATEWAY_TEST_REAL_S3") == "" {
		t.Skip("GATEWAY_TEST_REAL_S3 not set; skipping the check against real S3")
	}
	signer, err := blobstore.NewOffline(productionStorage)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Storage: mustRelay(productionStorage)}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const key = "relay-shape-check/never-written 文件.txt"

	put, err := signer.PresignedPut(ctx, key, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.relayUpload(w, relayRequest(http.MethodPut, put.String(), bytes.NewReader([]byte("x")), "text/plain").WithContext(ctx))
	expectS3RefusedTheKey(t, "PUT", put.Host, w)

	get, err := signer.PresignedGetAs(ctx, key, "文件.txt", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.relayDownload(w, relayRequest(http.MethodGet, get.String(), nil, "").WithContext(ctx))
	expectS3RefusedTheKey(t, "GET", get.Host, w)
}

func expectS3RefusedTheKey(t *testing.T, method, host string, w *httptest.ResponseRecorder) {
	t.Helper()
	env := decodeEnvelope(t, w)
	if w.Code != http.StatusBadGateway || env.Code != "STORAGE_RELAY_REJECTED" || !strings.Contains(env.Message, "InvalidAccessKeyId") {
		t.Fatalf("%s via %s: want S3's own 403 InvalidAccessKeyId relayed as 502 STORAGE_RELAY_REJECTED, got %d %s %q",
			method, host, w.Code, env.Code, env.Message)
	}
	t.Logf("%s reached %s; S3 answered: %s", method, host, env.Message)
}
