package app

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/sgao19/erp-go/pkg/blobstore"
)

// 一份放在内存里的对象存储，几个集成测试共用。
//
// 从前它长在 officepreview_integration_test.go 里。那个文件随「转成 PDF 再看」
// 那条路一起退役了（2026-09-14），而打包下载、在线 Office 取件这几条还要它，
// 所以搬到这里——共用的替身不该寄居在某一个用例的文件里。
//
// 没有的键报 blobstore.ErrNotFound，和真的存储一样：有人靠这个区分「没有」
// 和「存储不通」（recoverExcelResult）。
type previewStore struct {
	Files
	objects map[string][]byte
}

func (f *previewStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := f.objects[key]
	if !ok {
		return nil, fmt.Errorf("fake get %s: %w", key, blobstore.ErrNotFound)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (f *previewStore) Stat(_ context.Context, key string) (int64, string, error) {
	b, ok := f.objects[key]
	if !ok {
		return 0, "", fmt.Errorf("fake stat %s: %w", key, blobstore.ErrNotFound)
	}
	return int64(len(b)), "application/octet-stream", nil
}

func (f *previewStore) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.objects[key] = b
	return nil
}

func (f *previewStore) Remove(_ context.Context, key string) error {
	delete(f.objects, key)
	return nil
}

func (f *previewStore) PresignGetInline(_ context.Context, key, ct string) (string, error) {
	return "https://files.example/" + key + "?ct=" + ct, nil
}

// 嵌进来的 Files 是个 nil 接口：**没实现的方法一旦被调到就是空指针崩溃**，
// 不是编译错误。GetInbound 会走 signDownloads，所以这个也得有。
func (f *previewStore) PresignGet(_ context.Context, key, saveAs string) (string, error) {
	return "https://files.example/" + key + "?as=" + saveAs, nil
}
