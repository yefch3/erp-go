package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

func TestOrderSummaryContainsApprovalFacts(t *testing.T) {
	raw := orderSummary(store.GetPurchaseOrderRow{
		SupplierName: "测试钢厂", Currency: "USD", TotalAmount: "1250.00", ExpectedDate: "2026-09-30",
	}, []store.PurchaseOrderItemsRow{{ProductName: "钢卷", Qty: "5", UnitPrice: "250", Amount: "1250"}})
	var summary map[string]any
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		t.Fatalf("审批摘要不是合法 JSON: %v", err)
	}
	if summary["supplier"] != "测试钢厂" || summary["amount"] != "1250.00" {
		t.Fatalf("审批摘要缺少关键事实: %#v", summary)
	}
}

type supplierDirectoryStub struct {
	supplier Supplier
	err      error
}

func (s supplierDirectoryStub) Get(context.Context, int64) (Supplier, error) {
	return s.supplier, s.err
}

type warehouseDirectoryStub struct{ warehouse Warehouse }

func (w warehouseDirectoryStub) Get(context.Context, int64) (Warehouse, error) {
	return w.warehouse, nil
}

func TestSupplierForOrderRequiresAnActiveMasterRecord(t *testing.T) {
	t.Run("active supplier snapshot", func(t *testing.T) {
		svc := &Service{suppliers: supplierDirectoryStub{supplier: Supplier{
			ID: 7, Code: "SUP-7", Name: "Steel Mill", Status: "ACTIVE",
		}}}
		got, err := svc.supplierForOrder(context.Background(), 7)
		if err != nil || got.Code != "SUP-7" || got.Name != "Steel Mill" {
			t.Fatalf("got supplier %#v, err %v", got, err)
		}
	})

	t.Run("inactive supplier", func(t *testing.T) {
		svc := &Service{suppliers: supplierDirectoryStub{supplier: Supplier{
			ID: 7, Status: "INACTIVE",
		}}}
		if _, err := svc.supplierForOrder(context.Background(), 7); err == nil {
			t.Fatal("expected inactive supplier to be rejected")
		}
	})
}

func TestReceiveOrderRejectsAnInactiveWarehouseBeforeWriting(t *testing.T) {
	svc := &Service{warehouses: warehouseDirectoryStub{}}
	_, err := svc.ReceiveOrder(context.Background(), 1, 10, 99, []ReceiptLine{{POItemID: 1, Qty: "1"}}, "", Operator{})
	if err == nil || !strings.Contains(err.Error(), "仓库不存在或已停用") {
		t.Fatalf("expected inactive warehouse error, got %v", err)
	}
}

func TestReceiveOrderRejectsAVirtualWarehouseBeforeWriting(t *testing.T) {
	svc := &Service{warehouses: warehouseDirectoryStub{warehouse: Warehouse{
		ID: 99, Name: "Virtual warehouse", Type: "VIRTUAL", Status: "ACTIVE",
	}}}
	_, err := svc.ReceiveOrder(context.Background(), 1, 10, 99, []ReceiptLine{{POItemID: 1, Qty: "1"}}, "", Operator{})
	if err == nil || !strings.Contains(err.Error(), "实体仓库") {
		t.Fatalf("expected virtual warehouse error, got %v", err)
	}
}

func TestApprovalRejectReasonKeepsTheApproversComment(t *testing.T) {
	for _, tc := range []struct {
		name, result, comment, want string
	}{
		{name: "comment", result: "REJECTED", comment: "  单价需要重新确认  ", want: "单价需要重新确认"},
		{name: "returned", result: "RETURNED", want: "审批退回"},
		{name: "rejected", result: "REJECTED", want: "审批未通过"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := approvalRejectReason(tc.result, tc.comment); got != tc.want {
				t.Fatalf("approvalRejectReason() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestApprovalRequirementConflict(t *testing.T) {
	item := store.PurchaseOrderItemsForUpdateRow{
		RequirementID: 7,
		ProductName:   "热轧卷",
		Qty:           "5",
	}

	t.Run("available demand", func(t *testing.T) {
		req := store.RequirementsForOrderRow{ID: 7, Status: "PENDING", OpenQty: "5"}
		if got := approvalRequirementConflict([]store.RequirementsForOrderRow{req}, []store.PurchaseOrderItemsForUpdateRow{item}); got != "" {
			t.Fatalf("valid approval was rejected: %s", got)
		}
	})

	t.Run("partially ordered demand", func(t *testing.T) {
		req := store.RequirementsForOrderRow{ID: 7, Status: "PARTIALLY_ORDERED", OpenQty: "6"}
		if got := approvalRequirementConflict([]store.RequirementsForOrderRow{req}, []store.PurchaseOrderItemsForUpdateRow{item}); got != "" {
			t.Fatalf("valid partial demand was rejected: %s", got)
		}
	})

	t.Run("another order consumed the demand", func(t *testing.T) {
		req := store.RequirementsForOrderRow{ID: 7, Status: "ORDERED", OpenQty: "0"}
		got := approvalRequirementConflict([]store.RequirementsForOrderRow{req}, []store.PurchaseOrderItemsForUpdateRow{item})
		if !strings.Contains(got, "已经关闭或被其他采购单占用") {
			t.Fatalf("wrong conflict reason: %q", got)
		}
	})

	t.Run("contract change retired the demand", func(t *testing.T) {
		req := store.RequirementsForOrderRow{ID: 7, Status: "SUPERSEDED", OpenQty: "5"}
		got := approvalRequirementConflict([]store.RequirementsForOrderRow{req}, []store.PurchaseOrderItemsForUpdateRow{item})
		if !strings.Contains(got, "已经关闭或被其他采购单占用") {
			t.Fatalf("wrong superseded reason: %q", got)
		}
	})

	t.Run("not enough remains", func(t *testing.T) {
		req := store.RequirementsForOrderRow{ID: 7, Status: "PARTIALLY_ORDERED", OpenQty: "4.9999"}
		got := approvalRequirementConflict([]store.RequirementsForOrderRow{req}, []store.PurchaseOrderItemsForUpdateRow{item})
		if !strings.Contains(got, "剩余数量不足") {
			t.Fatalf("wrong shortage reason: %q", got)
		}
	})

	t.Run("requirement disappeared", func(t *testing.T) {
		got := approvalRequirementConflict(nil, []store.PurchaseOrderItemsForUpdateRow{item})
		if !strings.Contains(got, "不存在或发生变化") {
			t.Fatalf("wrong missing reason: %q", got)
		}
	})
}
