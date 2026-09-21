package grpcout

import (
	"context"
	"strings"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"google.golang.org/grpc"
)

type CustomerAccess struct {
	customers mdv1.CustomerServiceClient
	access    iamv1.AccessServiceClient
}

func NewCustomerAccess(md, iam *grpc.ClientConn) *CustomerAccess {
	return &CustomerAccess{customers: mdv1.NewCustomerServiceClient(md), access: iamv1.NewAccessServiceClient(iam)}
}
func (c *CustomerAccess) Check(ctx context.Context, id int64) (string, error) {
	resp, err := c.customers.GetCustomer(ctx, &mdv1.GetCustomerRequest{Id: id})
	if err != nil {
		return "", err
	}
	if resp.GetCustomer().GetStatus() != "ACTIVE" {
		return "", apierr.Invalid("MD_CUSTOMER_INACTIVE", "客户已停用")
	}
	return resp.GetCustomer().GetName(), nil
}
func (c *CustomerAccess) ResolveByName(ctx context.Context, name string) (int64, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, "", nil
	}
	resp, err := c.customers.ListCustomers(ctx, &mdv1.ListCustomersRequest{
		Page:    &commonv1.PageRequest{Page: 1, PageSize: 100},
		Keyword: name,
	})
	if err != nil {
		return 0, "", err
	}
	var id int64
	var canonical string
	for _, customer := range resp.GetCustomers() {
		if strings.EqualFold(strings.TrimSpace(customer.GetName()), name) {
			if id != 0 {
				return 0, "", nil
			}
			id, canonical = customer.GetId(), customer.GetName()
		}
	}
	return id, canonical, nil
}
func (c *CustomerAccess) VisibleIDs(ctx context.Context, actor int64) ([]int64, bool, error) {
	scope, err := c.access.VisibleEmployees(ctx, &iamv1.VisibleEmployeesRequest{EmployeeId: actor, Module: "customer"})
	if err != nil {
		return nil, false, err
	}
	if scope.GetAll() {
		return nil, true, nil
	}
	resp, err := c.customers.ListCustomerIdsByOwnerEmployees(ctx, &mdv1.ListCustomerIdsByOwnerEmployeesRequest{EmployeeIds: []int64{actor}})
	if err != nil {
		return nil, false, err
	}
	return resp.GetCustomerIds(), false, nil
}
