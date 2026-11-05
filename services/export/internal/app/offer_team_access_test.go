package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

type offerTeamScopes struct{}

func (offerTeamScopes) VisibleEmployees(_ context.Context, id int64, _ string) (Visibility, error) {
	if id == 3 {
		return Visibility{EmployeeIDs: []int64{1, 3}}, nil
	}
	return Visibility{EmployeeIDs: []int64{id}}, nil
}
func (offerTeamScopes) HasPermission(context.Context, int64, string) (bool, error) { return true, nil }

// The manager has document scope but no customer master ownership.
type offerTeamCustomers struct{}

func (offerTeamCustomers) VisibleIDs(_ context.Context, actor int64) ([]int64, bool, error) {
	if actor == 1 {
		return []int64{7}, false, nil
	}
	return nil, false, nil
}
func (offerTeamCustomers) Get(ctx context.Context, id int64) (Customer, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if op.EmployeeID == 1 && id == 7 {
		return Customer{ID: id, Name: "Customer", Status: "ACTIVE"}, nil
	}
	return Customer{}, apierr.NotFound("CUSTOMER_DENIED", "customer master denied")
}
