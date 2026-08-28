package app

import "testing"

func TestUsesPurchaseOrderFallback(t *testing.T) {
	tests := []struct {
		name    string
		bizType string
		want    bool
	}{
		{name: "采购单必须有人审批", bizType: "PURCHASE_ORDER", want: true},
		{name: "其他单据保留原有顶层自动通过规则", bizType: "EXPORT_CONTRACT", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := usesPurchaseOrderFallback(tt.bizType); got != tt.want {
				t.Fatalf("usesPurchaseOrderFallback(%q) = %v, want %v", tt.bizType, got, tt.want)
			}
		})
	}
}

func TestPreferOtherApprovers(t *testing.T) {
	if got := preferOtherApprovers([]int64{1, 2, 3}, 1); len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("有其他审批人时不应把任务交给提交人: %v", got)
	}
	if got := preferOtherApprovers([]int64{1}, 1); len(got) != 1 || got[0] != 1 {
		t.Fatalf("只有应急账号本人时也必须生成一条人工待办: %v", got)
	}
}

func TestSuperAdminOverrideIsLimitedToPurchaseOrders(t *testing.T) {
	svc := &Service{dir: stubDirectory{byCode: map[string][]int64{
		superAdminRoleCode: {500},
	}}}

	allowed, err := svc.canSuperAdminOverride(t.Context(), "PURCHASE_ORDER", 500)
	if err != nil || !allowed {
		t.Fatalf("最高权限管理员应能接管采购单审批: allowed=%v err=%v", allowed, err)
	}
	for _, tc := range []struct {
		name    string
		bizType string
		actorID int64
	}{
		{name: "普通员工不能接管采购单", bizType: "PURCHASE_ORDER", actorID: 600},
		{name: "管理员不能越权审批其他单据", bizType: "CONTRACT", actorID: 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allowed, err := svc.canSuperAdminOverride(t.Context(), tc.bizType, tc.actorID)
			if err != nil || allowed {
				t.Fatalf("不应允许越权审批: allowed=%v err=%v", allowed, err)
			}
		})
	}
}
