package grpcout

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/sgao19/erp-go/pkg/blobstore"
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
