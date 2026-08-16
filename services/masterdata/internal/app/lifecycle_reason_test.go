package app

import (
	"context"
	"testing"
)

// TestLifecycleReasonRequired 验证通过新接口执行启停时，后端不会接受空原因。
func TestLifecycleReasonRequired(t *testing.T) {
	svc := &Service{}
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "停用客户", run: func() error { return svc.DeactivateCustomer(context.Background(), 1, 1, 1, "管理员", "") }},
		{name: "恢复客户", run: func() error { return svc.ActivateCustomer(context.Background(), 1, 1, 1, "管理员", "") }},
		{name: "停用供应商", run: func() error { return svc.DeactivateSupplier(context.Background(), 1, 1, 1, "管理员", "") }},
		{name: "恢复供应商", run: func() error { return svc.ActivateSupplier(context.Background(), 1, 1, 1, "管理员", "") }},
		{name: "变更港口状态", run: func() error {
			_, err := svc.SetPortStatus(context.Background(), 1, 1, "INACTIVE", 1, 1, "管理员", "")
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Fatal("expected lifecycle reason validation error")
			}
		})
	}
}
