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
