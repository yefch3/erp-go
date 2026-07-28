package grpcout

import (
	"context"
	"time"

	"github.com/sgao19/erp-go/pkg/blobstore"
)

// Files adapts object storage to what the use cases need: a URL to PUT to and
// a URL to read from, with the expiry decided here rather than by callers.
type Files struct {
	store     *blobstore.Store
	putExpiry time.Duration
	getExpiry time.Duration
}

// A signed contract is often opened minutes after the list was loaded, so the
// read link outlives the upload link.
func NewFiles(store *blobstore.Store) *Files {
	return &Files{store: store, putExpiry: 10 * time.Minute, getExpiry: 30 * time.Minute}
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
