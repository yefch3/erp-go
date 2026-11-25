package grpcout

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/blobstore"
)

func TestOpenQualityObjectAgainstMinIO(t *testing.T) {
	endpoint := os.Getenv("GATEWAY_TEST_MINIO")
	if endpoint == "" {
		t.Skip("GATEWAY_TEST_MINIO not set; skipping object storage test")
	}
	env := func(name, fallback string) string {
		if value := os.Getenv(name); value != "" {
			return value
		}
		return fallback
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := blobstore.New(ctx, blobstore.Config{
		Endpoint: endpoint, PublicEndpoint: "unreachable.example.test:9000",
		AccessKey: env("MINIO_ACCESS_KEY", "erp"), SecretKey: env("MINIO_SECRET_KEY", "erp_dev_password"),
		Bucket: env("MINIO_BUCKET", "erp-files"),
	})
	if err != nil {
		t.Fatal(err)
	}
	files := NewFiles(store)
	key := "quality-read-test/" + strconv.FormatInt(time.Now().UnixNano(), 10) + "/质检 报告.txt"
	payload := []byte("synthetic quality report")
	if err := files.Put(ctx, key, payload, "text/plain"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = files.Remove(ctx, key) }()
	r, err := files.OpenQualityObject(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("read %q, err %v", got, err)
	}
	r, err = files.OpenQualityObject(ctx, key+".missing")
	if r != nil {
		_ = r.Close()
	}
	var apiError *apierr.Error
	if !errors.As(err, &apiError) || apiError.Code != "QUALITY_FILE_OBJECT_MISSING" {
		t.Fatalf("missing object error = %v", err)
	}
}
