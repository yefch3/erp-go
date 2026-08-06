package grpcout

import (
	"context"
	"io"
	"time"

	"github.com/sgao19/erp-go/pkg/blobstore"
)

// Files adapts the shared private object store to shipping's application port.
// Download capabilities are deliberately short lived and are created only
// after the gateway has checked the caller's document permission.
type Files struct{ store *blobstore.Store }

func NewFiles(store *blobstore.Store) *Files { return &Files{store: store} }

func (f *Files) PresignPut(ctx context.Context, key string) (string, int32, error) {
	u, err := f.store.PresignedPut(ctx, key, 10*time.Minute)
	if err != nil {
		return "", 0, err
	}
	return u.String(), 600, nil
}

func (f *Files) PresignDownload(ctx context.Context, key, fileName string) (string, error) {
	u, err := f.store.PresignedGetAs(ctx, key, fileName, 2*time.Minute)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (f *Files) PresignPreview(ctx context.Context, key, contentType string) (string, error) {
	u, err := f.store.PresignedGetInline(ctx, key, contentType, 2*time.Minute)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (f *Files) Stat(ctx context.Context, key string) (int64, string, error) {
	return f.store.Stat(ctx, key)
}

func (f *Files) ReadFile(ctx context.Context, key string, limit int64) ([]byte, error) {
	r, err := f.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, limit))
}

func (f *Files) Remove(ctx context.Context, key string) error { return f.store.Remove(ctx, key) }
