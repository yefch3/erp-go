package app

import (
	"context"
	"testing"

	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

type scopesStub struct {
	visibility Visibility
	err        error
}

func (s scopesStub) VisibleEmployees(context.Context, int64, string) (Visibility, error) {
	return s.visibility, s.err
}

type customerAccessStub struct {
	customerIDs []int64
	err         error
}

func (s customerAccessStub) CustomerIDsOwnedBy(context.Context, []int64) ([]int64, error) {
	return s.customerIDs, s.err
}

func TestVisibleScheduleByResponsibleOrCustomerOwner(t *testing.T) {
	customerID := int64(81)
	schedule := store.ShippingSchedule{ResponsibleEmployeeID: 9, CustomerID: &customerID}
	if !visibleSchedule(Visibility{EmployeeIDs: []int64{9}}, schedule) {
		t.Fatal("负责人自己的船期应当可见")
	}
	if !visibleSchedule(Visibility{CustomerIDs: []int64{81}}, schedule) {
		t.Fatal("负责该客户的员工应当能看到客户船期")
	}
	if visibleSchedule(Visibility{EmployeeIDs: []int64{10}, CustomerIDs: []int64{82}}, schedule) {
		t.Fatal("范围外船期不应可见")
	}
}

func TestAuthorizeAssignmentUsesIAMScope(t *testing.T) {
	service := &Service{}
	service.UseAccessControl(scopesStub{visibility: Visibility{EmployeeIDs: []int64{7, 8}, ScopeType: "DEPT"}}, customerAccessStub{})
	if err := service.authorizeAssignment(context.Background(), 8, Operator{ID: 7}); err != nil {
		t.Fatalf("部门范围内负责人被错误拒绝: %v", err)
	}
	if err := service.authorizeAssignment(context.Background(), 99, Operator{ID: 7}); errorCode(err) != "SHIPPING_RESPONSIBLE_OUT_OF_SCOPE" {
		t.Fatalf("范围外负责人错误码 = %q, err=%v", errorCode(err), err)
	}
}

func TestVisibleToIncludesOwnedCustomers(t *testing.T) {
	service := &Service{}
	service.UseAccessControl(
		scopesStub{visibility: Visibility{EmployeeIDs: []int64{7, 8}, ScopeType: "DEPT"}},
		customerAccessStub{customerIDs: []int64{41, 42}},
	)
	visibility, err := service.visibleTo(context.Background(), Operator{ID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(visibility.CustomerIDs) != 2 || visibility.CustomerIDs[0] != 41 {
		t.Fatalf("客户负责人范围未加入船期范围: %+v", visibility)
	}
}
