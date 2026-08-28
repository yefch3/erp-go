// Package grpcout 是 iam 对外部依赖的适配层。
//
// 名字沿用其他服务的惯例（product、shipping 都叫这个），尽管这里装的是对象
// 存储而不是 gRPC 客户端——统一的位置比精确的名字更有用，找的人知道去哪找。
package grpcout

import (
	"context"
	"time"

	"github.com/sgao19/erp-go/pkg/blobstore"
)

// Files 把 blobstore 包成 app.Files。
//
// 两个有效期是分开的，因为它们回答的是两个问题：上传要够一次选文件加一次
// 网络传输，读取要够一次页面加载和随后的几次刷新。把它们并成一个数，
// 就得取两者的最大值，于是上传链接白白多活了几分钟。
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

func (f *Files) Stat(ctx context.Context, key string) (int64, string, error) {
	return f.store.Stat(ctx, key)
}

func (f *Files) Remove(ctx context.Context, key string) error {
	return f.store.Remove(ctx, key)
}
