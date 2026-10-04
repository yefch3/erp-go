package grpcout

import (
	"context"
	"strings"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CustomerAccess struct {
	customers mdv1.CustomerServiceClient
	access    iamv1.AccessServiceClient
}

func customerSnapshotName(customer *mdv1.Customer) string {
	if customer == nil {
		return ""
	}
	if name := strings.TrimSpace(customer.GetShortName()); name != "" {
		return name
	}
	return strings.TrimSpace(customer.GetName())
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
	return customerSnapshotName(resp.GetCustomer()), nil
}

// CurrentNames reads a page of customers in one call. Until masterdata is on
// a release that has the batch RPC — the two services are not replaced in
// the same instant — it falls back to one GetCustomer per customer, which is
// what the list did before. The fallback can go once every environment runs
// a masterdata that serves GetCustomerNames (the release that introduced it).
func (c *CustomerAccess) CurrentNames(ctx context.Context, ids []int64) (map[int64]app.CustomerCurrentName, error) {
	out := make(map[int64]app.CustomerCurrentName, len(ids))
	resp, err := c.customers.GetCustomerNames(ctx, &mdv1.GetCustomerNamesRequest{Ids: ids})
	if status.Code(err) == codes.Unimplemented {
		for _, id := range ids {
			if _, done := out[id]; done {
				continue
			}
			one, getErr := c.customers.GetCustomer(ctx, &mdv1.GetCustomerRequest{Id: id})
			if status.Code(getErr) == codes.NotFound {
				continue
			}
			if getErr != nil {
				return nil, getErr
			}
			out[id] = app.CustomerCurrentName{Name: customerSnapshotName(one.GetCustomer()), Active: one.GetCustomer().GetStatus() == "ACTIVE"}
		}
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, customer := range resp.GetCustomers() {
		name := strings.TrimSpace(customer.GetShortName())
		if name == "" {
			name = strings.TrimSpace(customer.GetName())
		}
		out[customer.GetId()] = app.CustomerCurrentName{Name: name, Active: customer.GetStatus() == "ACTIVE"}
	}
	return out, nil
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
		if strings.EqualFold(strings.TrimSpace(customer.GetName()), name) || strings.EqualFold(strings.TrimSpace(customer.GetShortName()), name) {
			if id != 0 {
				return 0, "", nil
			}
			id, canonical = customer.GetId(), customerSnapshotName(customer)
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
