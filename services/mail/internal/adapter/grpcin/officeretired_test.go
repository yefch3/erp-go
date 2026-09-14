package grpcin

import (
	"context"
	"testing"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
)

// 「先转成 PDF 再预览」那条路 2026-09-14 退役了（在线 Office 接管之后它走不
// 到），但 rpc 本身还留在 proto 里——buf 用 FILE 档，删一个 rpc 是破坏性变更，
// 按仓库的两版规矩下一版再拿掉。
//
// 所以这里钉住的是「留着的那个不干活，而且明说自己不干活」：
//
//	· 网关那条路由已经撤了（routes_test 的 TestRetiredPdfPreviewRouteIsGone），
//	  但 gRPC 这一层还敞着——真有谁绕过网关直接打进来，该当场看见一句话，
//	  而不是拿到一个 nil 或者一个点不开的空地址。
//	· 也挡一种回头路：谁要是把实现接回去，这条会红，逼他先想清楚为什么。
func TestRetiredPdfPreviewSaysSoInsteadOfAnsweringEmpty(t *testing.T) {
	h := &Handler{}
	resp, err := h.PreviewInboundAttachment(context.Background(),
		&mailv1.PreviewInboundAttachmentRequest{InboundId: 1, AttachmentId: 2})
	if resp != nil {
		t.Fatalf("退役了的方法不该给出响应：%+v", resp)
	}
	if code := apierr.CodeFromError(err); code != "MAIL_PREVIEW_RETIRED" {
		t.Fatalf("该明说已退役，得到 %q（%v）", code, err)
	}
}
