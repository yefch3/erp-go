package grpcin

import (
	"context"
	"errors"
	"testing"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"google.golang.org/grpc"
)

type supplierIAM struct {
	iamv1.AccessServiceClient
	failure error
}

func (s supplierIAM) VisibleEmployees(ctx context.Context, req *iamv1.VisibleEmployeesRequest, _ ...grpc.CallOption) (*iamv1.VisibleEmployeesResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if req.Module != "supplier" || req.EmployeeId != op.EmployeeID {
		return nil, errors.New("wrong supplier scope identity")
	}
	return &iamv1.VisibleEmployeesResponse{All: op.EmployeeID == 99}, s.failure
}

func TestSupplierAccessFailsClosedAndRestrictsDeletion(t *testing.T) {
	h := New(nil)
	called := false
	next := func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/erp.masterdata.v1.SupplierService/DeactivateSupplier"}
	if _, err := h.SupplierAccess(supplierIAM{})(context.Background(), &mdv1.DeactivateSupplierRequest{Id: 1}, info, next); err == nil {
		t.Fatal("missing identity accepted")
	}
	ordinary := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 1, EmployeeID: 1})
	if _, err := h.SupplierAccess(supplierIAM{failure: errors.New("IAM unavailable")})(ordinary, &mdv1.ListSuppliersRequest{}, info, next); err == nil {
		t.Fatal("IAM failure accepted")
	}
	if _, err := h.SupplierAccess(supplierIAM{})(ordinary, &mdv1.DeactivateSupplierRequest{Id: 1}, info, next); err == nil {
		t.Fatal("ordinary supplier deletion accepted")
	}
	highest := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 1, EmployeeID: 99})
	if _, err := h.SupplierAccess(supplierIAM{})(highest, &mdv1.DeactivateSupplierRequest{Id: 1}, info, next); err != nil {
		t.Fatalf("highest supplier deletion denied: %v", err)
	}
	if !called {
		t.Fatal("highest deletion did not reach handler")
	}
}
