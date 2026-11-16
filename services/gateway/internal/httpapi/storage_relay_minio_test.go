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

// 对着真的对象存储（本地 MinIO）走一遍：签地址 → 中转上传 → 存储里真有 →
// 签读地址 → 中转下载 → 字节一致 → 删掉。
//
// 假存储的测试证明中转没改动请求；这个测试证明「没改动」在真签名面前成立——
// 中文文件名的百分号编码、保留下来的 Host 头、查询串里的签名，只有真存储会
// 逐一核对。`make test-integration` 带着 GATEWAY_TEST_MINIO 跑；没有就跳过。
func TestStorageRelayAgainstMinIO(t *testing.T) {
	endpoint := os.Getenv("GATEWAY_TEST_MINIO")
	if endpoint == "" {
		t.Skip("GATEWAY_TEST_MINIO not set; skipping MinIO-backed relay test")
	}
	env := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	}
	bucket := env("MINIO_BUCKET", "erp-files")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// 一份配置，两头用：业务服务拿它签地址，网关拿它认地址。生产上也是这个关系。
	cfg := blobstore.Config{
		Endpoint:  endpoint,
		AccessKey: env("MINIO_ACCESS_KEY", "erp"),
		SecretKey: env("MINIO_SECRET_KEY", "erp_dev_password"),
		Bucket:    bucket,
	}
	store, err := blobstore.New(ctx, cfg)
	if err != nil {
		t.Fatalf("minio at %s: %v", endpoint, err)
	}
	s := &Server{Storage: mustRelay(cfg)}
	key := "relay-test/" + time.Now().UTC().Format("20060102T150405.000000000") + "-质检 照片 (1).jpg"
	t.Cleanup(func() { _ = store.Remove(context.Background(), key) })

	putURL, err := store.PresignedPut(ctx, key, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("not really a jpeg, but 42 bytes of payload!!")
	w := httptest.NewRecorder()
	s.relayUpload(w, relayRequest(http.MethodPut, putURL.String(), bytes.NewReader(payload), "image/jpeg"))
	if w.Code != http.StatusOK {
		t.Fatalf("relay upload: %d %s", w.Code, w.Body.String())
	}

	size, contentType, err := store.Stat(ctx, key)
	if err != nil {
		t.Fatalf("object missing after relay upload: %v", err)
	}
	if size != int64(len(payload)) || contentType != "image/jpeg" {
		t.Fatalf("stored size %d type %q", size, contentType)
	}

	getURL, err := store.PresignedGetAs(ctx, key, "质检照片.jpg", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.relayDownload(w, relayRequest(http.MethodGet, getURL.String(), nil, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("relay download: %d %s", w.Code, w.Body.String())
	}
	if !bytes.Equal(w.Body.Bytes(), payload) {
		t.Fatalf("payload changed on the way back: %q", w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") || !strings.Contains(cd, "%E8%B4%A8%E6%A3%80") {
		t.Fatalf("Content-Disposition %q should be an attachment carrying the signed-in name", cd)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("Content-Type %q", ct)
	}
}
