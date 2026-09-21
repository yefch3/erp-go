package grpcin

import (
	"context"
	"strings"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
	"google.golang.org/grpc"
)

// SupplierAccess supplies the authenticated employee scope to document
// reminders and enforces the highest-role-only supplier deletion rule.
func (h *Handler) SupplierAccess(access iamv1.AccessServiceClient) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		if !strings.HasPrefix(info.FullMethod, "/erp.masterdata.v1.SupplierService/") {
			return next(ctx, req)
		}
		op, ok := grpcx.OperatorFromContext(ctx)
		if !ok || op.TenantID <= 0 || op.EmployeeID <= 0 || access == nil {
			return nil, apierr.Permission("MD_SUPPLIER_ACCESS_DENIED", "供应商访问需要登录用户")
		}
		visibility, err := access.VisibleEmployees(ctx, &iamv1.VisibleEmployeesRequest{EmployeeId: op.EmployeeID, Module: "supplier"})
		if err != nil {
			return nil, err
		}
		employeeID := op.EmployeeID
		if visibility.GetAll() {
			employeeID = 0
		}
		ctx = app.WithSupplierAccess(ctx, employeeID)
		if _, deleting := req.(*mdv1.DeactivateSupplierRequest); deleting && employeeID != 0 {
			return nil, apierr.Permission("MD_SUPPLIER_DELETE_DENIED", "只有最高权限用户可以删除供应商")
		}
		return next(ctx, req)
	}
}
