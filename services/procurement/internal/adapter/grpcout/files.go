package grpcout

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/blobstore"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

// Files adapts blobstore to the app's Files port, same shape as product's.
// Upload URLs are short-lived because they are a capability: whoever holds
// one can write that object.
type Files struct {
	store     *blobstore.Store
	putExpiry time.Duration
	getExpiry time.Duration
}

func NewFiles(store *blobstore.Store) *Files {
	return &Files{store: store, putExpiry: 10 * time.Minute, getExpiry: 15 * time.Minute}
}

func (f *Files) PresignPut(ctx context.Context, key string) (string, int32, error) {
	u, err := f.store.PresignedPut(ctx, key, f.putExpiry)
	if err != nil {
		return "", 0, err
	}
	return u.String(), int32(f.putExpiry.Seconds()), nil
}

func (f *Files) Put(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := f.store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), contentType)
	return err
}

func (f *Files) PresignGet(ctx context.Context, key string) (string, error) {
	u, err := f.store.PresignedGet(ctx, key, f.getExpiry)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (f *Files) Remove(ctx context.Context, key string) error {
	return f.store.Remove(ctx, key)
}

func (f *Files) Read(ctx context.Context, key string) ([]byte, error) {
	r, err := f.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func (f *Files) OpenQualityObject(ctx context.Context, key string) (io.ReadCloser, error) {
	size, _, err := f.store.Stat(ctx, key)
	if errors.Is(err, blobstore.ErrNotFound) {
		return nil, apierr.NotFound("QUALITY_FILE_OBJECT_MISSING", "存储中找不到这份附件，请联系上传人核对")
	}
	if err != nil {
		return nil, err
	}
	if size > app.MaxQualityFileBytes {
		return nil, apierr.Invalid("QUALITY_FILE_TOO_LARGE", "附件超过 64 MB，无法读取")
	}
	return f.store.Get(ctx, key)
}
