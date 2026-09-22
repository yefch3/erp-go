package app

import (
	"context"
	"testing"
)

func TestNormalizeBulkOwnerInput(t *testing.T) {
	entities, owners, action, err := normalizeBulkOwnerInput(
		[]int64{2, 2, 3, 0},
		[]BulkOwnerAssignment{{EmployeeID: 8, EmployeeName: " 销售 A "}, {EmployeeID: 8, EmployeeName: "重复"}, {EmployeeID: 9, EmployeeName: "销售 B"}},
		" add ",
	)
	if err != nil {
		t.Fatal(err)
	}
	if action != "ADD" || len(entities) != 2 || len(owners) != 2 || owners[0].EmployeeName != "销售 A" {
		t.Fatalf("unexpected normalized input: entities=%v owners=%v action=%s", entities, owners, action)
	}
}

func TestNormalizeBulkOwnerInputRejectsInvalidRequest(t *testing.T) {
	if _, _, _, err := normalizeBulkOwnerInput([]int64{1}, []BulkOwnerAssignment{{EmployeeID: 2, EmployeeName: "A"}}, "replace"); err == nil {
		t.Fatal("expected unsupported action to fail")
	}
	if _, _, _, err := normalizeBulkOwnerInput(nil, []BulkOwnerAssignment{{EmployeeID: 2, EmployeeName: "A"}}, "add"); err == nil {
		t.Fatal("expected empty entity selection to fail")
	}
}

func TestBulkOwnerChangesRequireHighestPermission(t *testing.T) {
	service := &Service{}
	owner := []BulkOwnerAssignment{{EmployeeID: 8, EmployeeName: "A"}}
	if _, err := service.BatchUpdateCustomerOwners(WithCustomerAccess(context.Background(), 99), 1, []int64{1}, owner, "ADD", 99, "普通销售"); err == nil {
		t.Fatal("ordinary sales user must not batch update customer owners")
	}
	if _, err := service.BatchUpdateSupplierOwners(WithSupplierAccess(context.Background(), 99), 1, []int64{1}, owner, "ADD", 99, "普通采购"); err == nil {
		t.Fatal("ordinary procurement user must not batch update supplier owners")
	}
}
