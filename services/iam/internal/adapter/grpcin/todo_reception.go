package grpcin

import (
	"context"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/iam/internal/app"
)

func todoTarget(ctx context.Context, roleID, employeeID int64) error {
	if grpcx.TenantID(ctx) <= 0 {
		return apierr.Unauthorized("IAM_TODO_TENANT", "请先登录")
	}
	if roleID < 0 || employeeID < 0 || (roleID == 0) == (employeeID == 0) {
		return apierr.Invalid("IAM_TODO_TARGET", "请选择单个角色或员工")
	}
	return nil
}

func todoReceptionProto(reception app.TodoReception) *iamv1.TodoReception {
	return &iamv1.TodoReception{ApprovalsOnly: reception.ApprovalsOnly, Categories: reception.Categories, EmployeeOverride: reception.EmployeeOverride}
}

func (h *Handler) GetTodoReception(ctx context.Context, req *iamv1.GetTodoReceptionRequest) (*iamv1.GetTodoReceptionResponse, error) {
	if err := todoTarget(ctx, req.GetRoleId(), req.GetEmployeeId()); err != nil {
		return nil, err
	}
	tenant := grpcx.TenantID(ctx)
	var reception app.TodoReception
	var err error
	if req.GetRoleId() > 0 {
		reception, err = h.svc.GetRoleTodoReception(ctx, tenant, req.GetRoleId())
	} else {
		reception, err = h.svc.GetEmployeeTodoReception(ctx, tenant, req.GetEmployeeId())
	}
	if err != nil {
		return nil, err
	}
	return &iamv1.GetTodoReceptionResponse{Reception: todoReceptionProto(reception)}, nil
}

func (h *Handler) SetTodoReception(ctx context.Context, req *iamv1.SetTodoReceptionRequest) (*iamv1.SetTodoReceptionResponse, error) {
	if err := todoTarget(ctx, req.GetRoleId(), req.GetEmployeeId()); err != nil {
		return nil, err
	}
	tenant := grpcx.TenantID(ctx)
	var reception app.TodoReception
	var err error
	if req.GetRoleId() > 0 {
		reception, err = h.svc.SetRoleTodoReception(ctx, tenant, req.GetRoleId(), app.TodoReception{ApprovalsOnly: req.GetApprovalsOnly(), Categories: req.GetCategories()})
	} else {
		if len(req.GetCategories()) != 0 {
			return nil, apierr.Invalid("IAM_TODO_EMPLOYEE_CATEGORIES", "员工设置支持继承角色或仅审批")
		}
		reception, err = h.svc.SetEmployeeTodoReception(ctx, tenant, req.GetEmployeeId(), req.GetApprovalsOnly())
	}
	if err != nil {
		return nil, err
	}
	return &iamv1.SetTodoReceptionResponse{Reception: todoReceptionProto(reception)}, nil
}
