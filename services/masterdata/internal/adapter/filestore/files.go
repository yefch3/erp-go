// Package filestore adapts blobstore to the app's Files port. Masterdata's
// only outbound dependency, so it gets its own package instead of a
// misnamed grpcout.
package filestore

import (
	"context"
	"time"

	"github.com/sgao19/erp-go/pkg/blobstore"
)

// Files presigns URLs; upload URLs are short-lived because they are a
// capability: whoever holds one can write that object.
type Files struct {
	store     *blobstore.Store
	putExpiry time.Duration
	getExpiry time.Duration
}

func New(store *blobstore.Store) *Files {
	return &Files{store: store, putExpiry: 10 * time.Minute, getExpiry: 15 * time.Minute}
}

func (f *Files) PresignPut(ctx context.Context, key string) (string, int32, error) {
	u, err := f.store.PresignedPut(ctx, key, f.putExpiry)
	if err != nil {
		return "", 0, err
	}
	return u.String(), int32(f.putExpiry.Seconds()), nil
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
