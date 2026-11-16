package app

import (
	"context"
	"testing"

	"github.com/sgao19/erp-go/pkg/grpcx"
)

type deliveryOnlyPermission string

func (p deliveryOnlyPermission) Allowed(_ context.Context, _ int64, permission string) (bool, error) {
	return string(p) == permission, nil
}

func TestDeliveryQualityGateRequiresActorTenantAndPermission(t *testing.T) {
	svc := &Service{}
	if err := svc.AuthorizeDeliveryQualityCheck(context.Background(), 1); code(err) != "QUALITY_DELIVERY_IDENTITY" {
		t.Fatal(err)
	}
	ctx := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 1, EmployeeID: 77})
	if err := svc.AuthorizeDeliveryQualityCheck(ctx, 2); code(err) != "QUALITY_DELIVERY_TENANT" {
		t.Fatal(err)
	}
	if err := svc.AuthorizeDeliveryQualityCheck(ctx, 1); code(err) != "QUALITY_DELIVERY_AUTH" {
		t.Fatal(err)
	}
	svc.permissions = &deniedDailyPermission{}
	if err := svc.AuthorizeDeliveryQualityCheck(ctx, 1); code(err) != "QUALITY_DELIVERY_FORBIDDEN" {
		t.Fatal(err)
	}
	svc.permissions = dailyAllowAll{}
	if err := svc.AuthorizeDeliveryQualityCheck(ctx, 1); err != nil {
		t.Fatal(err)
	}
	for _, permission := range []string{"inventory:stock:write", "procurement:receipt:write", "shipping:progress:write", "shipping:route:write", "shipping:schedule:write", "export:shipment:write"} {
		svc.permissions = deliveryOnlyPermission(permission)
		if err := svc.AuthorizeDeliveryQualityCheck(ctx, 1); err != nil {
			t.Fatalf("permission %s rejected: %v", permission, err)
		}
	}
}
