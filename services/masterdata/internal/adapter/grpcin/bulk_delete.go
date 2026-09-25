package grpcin

import (
	"context"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
)

type BulkDeleteHandler struct {
	mdv1.UnimplementedMasterDataBulkDeleteServiceServer
	Service *app.Service
	Access  iamv1.AccessServiceClient
}

func (h *BulkDeleteHandler) BulkDeleteMasterData(ctx context.Context, in *mdv1.BulkDeleteMasterDataRequest) (*mdv1.BulkDeleteMasterDataResponse, error) {
	op, ok := grpcx.OperatorFromContext(ctx)
	if !ok || op.TenantID <= 0 || op.EmployeeID <= 0 || h.Access == nil {
		return nil, apierr.Permission("MASTER_DELETE_AUTH", "仅最高权限管理员可批量删除")
	}
	// IAM reserves the customer ALL scope for an active SUPER_ADMIN, unlike
	// ordinary modules where a department manager may also have an ALL scope.
	scope, err := h.Access.VisibleEmployees(ctx, &iamv1.VisibleEmployeesRequest{EmployeeId: op.EmployeeID, Module: "customer"})
	if err != nil {
		return nil, err
	}
	if !scope.GetAll() {
		return nil, apierr.Permission("MASTER_DELETE_AUTH", "仅最高权限管理员可批量删除")
	}
	input := app.BulkDeleteInput{Entity: in.Entity, StartAt: in.StartAt, EndAt: in.EndAt, Execute: in.Execute}
	for _, item := range in.Selections {
		input.Selections = append(input.Selections, app.BulkDeleteSelection{ID: item.Id, Version: item.Version})
	}
	result, err := h.Service.BulkDelete(ctx, op.TenantID, op.EmployeeID, op.Name, input)
	if err != nil {
		return nil, err
	}
	out := &mdv1.BulkDeleteMasterDataResponse{DeletedCount: result.DeletedCount}
	for _, row := range result.Rows {
		out.Rows = append(out.Rows, &mdv1.BulkDeleteMasterDataRow{Id: row.ID, Code: row.Code, Name: row.Name, CreatedAt: row.CreatedAt, Version: row.Version, BlockedReason: row.BlockedReason})
	}
	return out, nil
}
