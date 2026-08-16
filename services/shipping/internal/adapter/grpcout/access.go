package grpcout

import (
	"context"

	"google.golang.org/grpc"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/services/shipping/internal/app"
)

type Scopes struct{ client iamv1.AccessServiceClient }

func NewScopes(conn *grpc.ClientConn) *Scopes {
	return &Scopes{client: iamv1.NewAccessServiceClient(conn)}
}

func (s *Scopes) VisibleEmployees(ctx context.Context, employeeID int64, module string) (app.Visibility, error) {
	resp, err := s.client.VisibleEmployees(ctx, &iamv1.VisibleEmployeesRequest{
		EmployeeId: employeeID, Module: module,
	})
	if err != nil {
		return app.Visibility{}, err
	}
	return app.Visibility{
		All: resp.GetAll(), EmployeeIDs: resp.GetEmployeeIds(), ScopeType: resp.GetScopeType(),
	}, nil
}

type CustomerAccess struct{ client mdv1.CustomerServiceClient }

func NewCustomerAccess(conn *grpc.ClientConn) *CustomerAccess {
	return &CustomerAccess{client: mdv1.NewCustomerServiceClient(conn)}
}

func (c *CustomerAccess) CustomerIDsOwnedBy(ctx context.Context, employeeIDs []int64) ([]int64, error) {
	resp, err := c.client.ListCustomerIdsByOwnerEmployees(ctx, &mdv1.ListCustomerIdsByOwnerEmployeesRequest{
		EmployeeIds: employeeIDs,
	})
	if err != nil {
		return nil, err
	}
	return resp.GetCustomerIds(), nil
}
