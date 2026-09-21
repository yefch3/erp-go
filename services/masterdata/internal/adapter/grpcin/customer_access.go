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

// CustomerAccess protects every customer RPC, including calls from other
// services. IAM determines the exception; masterdata owns the owner relation.
func (h *Handler) CustomerAccess(access iamv1.AccessServiceClient) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		customerMethod := strings.HasPrefix(info.FullMethod, "/erp.masterdata.v1.CustomerService/")
		var customerID int64
		if party, ok := req.(interface {
			GetPartyType() string
			GetPartyId() int64
		}); ok && strings.EqualFold(strings.TrimSpace(party.GetPartyType()), "CUSTOMER") {
			customerMethod, customerID = true, party.GetPartyId()
		}
		if !customerMethod {
			return next(ctx, req)
		}
		op, ok := grpcx.OperatorFromContext(ctx)
		if !ok || op.TenantID <= 0 || op.EmployeeID <= 0 || access == nil {
			return nil, apierr.Permission("MD_CUSTOMER_ACCESS_DENIED", "客户访问需要登录用户")
		}
		visibility, err := access.VisibleEmployees(ctx, &iamv1.VisibleEmployeesRequest{EmployeeId: op.EmployeeID, Module: "customer"})
		if err != nil {
			return nil, err
		}
		employeeID := op.EmployeeID
		if visibility.GetAll() {
			employeeID = 0
		}
		ctx = app.WithCustomerAccess(ctx, employeeID)
		if _, deleting := req.(*mdv1.DeactivateCustomerRequest); deleting && employeeID != 0 {
			return nil, apierr.Permission("MD_CUSTOMER_DELETE_DENIED", "只有最高权限用户可以删除客户")
		}
		if relation, ok := req.(interface{ GetCustomerId() int64 }); ok {
			customerID = relation.GetCustomerId()
		} else if customer, ok := req.(interface{ GetId() int64 }); ok {
			customerID = customer.GetId()
		}
		if customerID > 0 {
			if err := h.svc.AuthorizeCustomer(ctx, op.TenantID, customerID); err != nil {
				return nil, err
			}
		}
		return next(ctx, req)
	}
}
