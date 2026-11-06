package app

import (
	"context"
	"testing"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

type executionOnlyScopes struct{ permission string }

func (executionOnlyScopes) VisibleEmployees(context.Context, int64, string) (Visibility, error) {
	return Visibility{}, nil
}

func (s executionOnlyScopes) HasPermission(_ context.Context, _ int64, code string) (bool, error) {
	return code == s.permission, nil
}

func TestDeliveryOperatorsCanCheckExecutionWithoutReadingCommercialData(t *testing.T) {
	ctx := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 17, EmployeeID: 23})
	for _, permission := range []string{"procurement:receipt:write", "inventory:stock:write", "shipping:progress:write", "shipping:route:write"} {
		t.Run(permission, func(t *testing.T) {
			svc := New(nil, Deps{Scopes: executionOnlyScopes{permission: permission}})
			if err := svc.AuthorizeContractExecutionCheck(ctx); err != nil {
				t.Fatalf("legitimate delivery operator cannot check execution: %v", err)
			}
			if err := svc.RequireAnyPermission(ctx, "export:contract:read", "export:receipt:read"); err == nil {
				t.Fatal("execution verdict granted commercial data access")
			}
			if _, _, err := svc.CheckContractExecution(ctx, 18, 1); apierr.CodeFromError(err) != "EX_TENANT_MISMATCH" {
				t.Fatalf("cross-tenant check reached the database: %v", err)
			}
		})
	}
	svc := New(nil, Deps{Scopes: executionOnlyScopes{permission: "inventory:stock:read"}})
	if _, _, err := svc.CheckContractExecution(ctx, 17, 1); err == nil {
		t.Fatal("read-only operator reached the execution query")
	}
	if _, _, err := svc.CheckContractExecution(context.Background(), 17, 1); err == nil {
		t.Fatal("anonymous operator reached the execution query")
	}
}
