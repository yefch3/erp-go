package grpcout

import (
	"context"
	"testing"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"google.golang.org/grpc"
)

type reimbursementDepartments struct {
	iamv1.DirectoryServiceClient
}

func (reimbursementDepartments) GetEmployee(_ context.Context, req *iamv1.GetEmployeeRequest, _ ...grpc.CallOption) (*iamv1.GetEmployeeResponse, error) {
	return &iamv1.GetEmployeeResponse{Employee: &iamv1.Employee{DepartmentId: req.Id}}, nil
}

func (reimbursementDepartments) ListDepartments(context.Context, *iamv1.ListDepartmentsRequest, ...grpc.CallOption) (*iamv1.ListDepartmentsResponse, error) {
	return &iamv1.ListDepartmentsResponse{Departments: []*iamv1.Department{
		{Id: 1, Code: "001", Name: "海运部", Status: "ACTIVE"},
		{Id: 2, Code: "PROCUREMENT", Name: "采购运营", Status: "ACTIVE"},
		{Id: 3, Code: "QUALITY", Name: "质检部", Status: "ACTIVE"},
		{Id: 4, Code: "SALES", Name: "天津", Status: "ACTIVE"},
		{Id: 5, Code: "SHIPPING", Name: "海运部", Status: "INACTIVE"},
	}}, nil
}

func TestDirectReimbursementUsesCurrentDepartment(t *testing.T) {
	client := &IAM{directory: reimbursementDepartments{}}
	for id, want := range map[int64]bool{1: true, 2: true, 3: true, 4: false, 5: false, 6: false} {
		got, err := client.DirectReimbursementDepartment(context.Background(), id)
		if err != nil || got != want {
			t.Errorf("employee %d = %v, want %v, error %v", id, got, want, err)
		}
	}
}
