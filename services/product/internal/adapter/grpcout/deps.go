// Package grpcout holds product's outbound dependencies: the numbering
// service it borrows codes from, and the object storage it presigns URLs for.
package grpcout

import (
	"context"
	"time"

	"google.golang.org/grpc"

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/blobstore"
)

// Numbering borrows masterdata's sequence generator so product codes come
// from the same place as every other document number.
type Numbering struct {
	client mdv1.NumberingServiceClient
}

func NewNumbering(conn *grpc.ClientConn) *Numbering {
	return &Numbering{client: mdv1.NewNumberingServiceClient(conn)}
}

func (n *Numbering) Next(ctx context.Context, bizType string) (string, error) {
	resp, err := n.client.NextNumber(ctx, &mdv1.NextNumberRequest{BizType: bizType})
	if err != nil {
		return "", err
	}
	return resp.GetNumber(), nil
}

// Files adapts blobstore to the app's Files port. Upload URLs are short-lived
// because they are a capability: whoever holds one can write that object.
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
