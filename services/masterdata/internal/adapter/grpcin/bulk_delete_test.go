package grpcin

import (
	"context"
	"errors"
	"testing"

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func TestBulkDeleteRPCDeniesMissingIdentityAndIAMFailure(t *testing.T) {
	for _, ctx := range []context.Context{context.Background(), grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 1, EmployeeID: 7})} {
		h := &BulkDeleteHandler{Access: customerIAM{failure: errors.New("IAM down")}}
		if _, err := h.BulkDeleteMasterData(ctx, &mdv1.BulkDeleteMasterDataRequest{}); err == nil {
			t.Fatal("unauthorized call accepted")
		}
	}
}

func TestBulkDeleteRPCDeniesOrdinaryUser(t *testing.T) {
	ctx := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 1, EmployeeID: 7})
	h := &BulkDeleteHandler{Access: customerIAM{}}
	if _, err := h.BulkDeleteMasterData(ctx, &mdv1.BulkDeleteMasterDataRequest{}); err == nil {
		t.Fatal("ordinary user reached deletion service")
	}
}
