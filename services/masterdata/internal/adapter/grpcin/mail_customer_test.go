package grpcin

import (
	"context"
	"errors"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"google.golang.org/grpc"
	"testing"
)

type mailCustomerIAM struct {
	iamv1.AccessServiceClient
	allow   bool
	failure bool
}

func (s mailCustomerIAM) CheckPermission(_ context.Context, r *iamv1.CheckPermissionRequest, _ ...grpc.CallOption) (*iamv1.CheckPermissionResponse, error) {
	if s.failure {
		return nil, errors.New("IAM down")
	}
	return &iamv1.CheckPermissionResponse{Allowed: s.allow && r.PermissionCode == "masterdata:customer:write"}, nil
}
func (s mailCustomerIAM) VisibleEmployees(context.Context, *iamv1.VisibleEmployeesRequest, ...grpc.CallOption) (*iamv1.VisibleEmployeesResponse, error) {
	return &iamv1.VisibleEmployeesResponse{}, nil
}
func TestMailCustomerRPCRequiresWritePermission(t *testing.T) {
	h := New(nil)
	info := &grpc.UnaryServerInfo{FullMethod: "/erp.masterdata.v1.CustomerService/SaveMailCustomer"}
	ctx := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 1, EmployeeID: 7})
	for _, access := range []mailCustomerIAM{{}, {allow: true}, {allow: true, failure: true}} {
		called := false
		_, err := h.CustomerAccess(access)(ctx, &mdv1.SaveMailCustomerRequest{}, info, func(context.Context, any) (any, error) { called = true; return nil, nil })
		want := access.allow && !access.failure
		if called != want || (err == nil) != want {
			t.Fatalf("permission boundary failed: called=%v err=%v", called, err)
		}
	}
}
