package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

type officePermStub struct {
	iamv1.AccessServiceClient
	allowed map[string]bool
}

func (s officePermStub) CheckPermission(_ context.Context, req *iamv1.CheckPermissionRequest, _ ...grpc.CallOption) (*iamv1.CheckPermissionResponse, error) {
	return &iamv1.CheckPermissionResponse{Allowed: s.allowed[req.GetPermissionCode()]}, nil
}

// 记下网关到底给邮件服务转了什么。
type officeEmailsStub struct {
	mailv1.EmailServiceClient
	sawEdit bool
}

func (s *officeEmailsStub) OfficePreviewConfig(_ context.Context, req *mailv1.OfficePreviewConfigRequest, _ ...grpc.CallOption) (*mailv1.OfficePreviewConfigResponse, error) {
	s.sawEdit = req.GetEdit()
	return &mailv1.OfficePreviewConfigResponse{DocsUrl: "/docs", ConfigJson: "{}", Token: "t", Editable: req.GetEdit()}, nil
}

// 在线 Office 那条路由挂在 mail:email:read 上，但带 edit=1 时它是**写**：
// 签出来的配置带回存地址，人改完会在库里落一条新版本。
//
// 只读和写在这套系统里是分得开的两个权限（有"只读审计"这样的角色）。没有
// 这一层判断的话，一个只被授予读的人就能给客户的附件添版本——而且全程不
// 需要绕过任何密码学的门，纯粹是权限漏判。
func TestOfficeEditNeedsWritePermission(t *testing.T) {
	cases := []struct {
		name    string
		perms   map[string]bool
		query   string
		wantEdt bool
	}{
		{"读 + 写，要编辑就给编辑", map[string]bool{"mail:email:read": true, "mail:email:write": true}, "?edit=1", true},
		{"只有读，要编辑也只给只读", map[string]bool{"mail:email:read": true}, "?edit=1", false},
		{"有写权限但没要编辑，照样只读", map[string]bool{"mail:email:read": true, "mail:email:write": true}, "", false},
		// edit=0 和没有这个参数是一个意思，别让第二种写法自己长出来。
		{"edit=0 不算要编辑", map[string]bool{"mail:email:read": true, "mail:email:write": true}, "?edit=0", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			emails := &officeEmailsStub{}
			s := &Server{Access: officePermStub{allowed: c.perms}, Emails: emails}
			req := httptest.NewRequest(http.MethodGet, "/api/inbound-mails/7/attachments/9/office"+c.query, nil)
			req = req.WithContext(grpcx.WithOperator(req.Context(), grpcx.Operator{TenantID: 1, EmployeeID: 9}))
			rec := httptest.NewRecorder()
			s.officePreviewConfig(rec, req)

			// 没有写权限时**退回只读**而不是 403：人点开这一页要的是看这份
			// 文件，而「编辑」那颗按钮会因为 editable=false 自己消失。
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if emails.sawEdit != c.wantEdt {
				t.Fatalf("转给邮件服务的 edit=%v，应该是 %v", emails.sawEdit, c.wantEdt)
			}
		})
	}
}
