package grpcin

import (
	"context"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/iam/internal/app"
)

// 平台层入口。守卫（platform_operators 名单）在 app 层的每个方法里自查，
// 这里只负责把调用者身份递进去——网关转发什么就是什么，不在此复述规则。

func (h *Handler) CheckOperator(ctx context.Context, _ *iamv1.CheckOperatorRequest) (*iamv1.CheckOperatorResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	ok, err := h.svc.IsPlatformOperator(ctx, op.EmployeeID)
	if err != nil {
		return nil, err
	}
	return &iamv1.CheckOperatorResponse{Operator: ok}, nil
}

func (h *Handler) ListPlatformTenants(ctx context.Context, _ *iamv1.ListPlatformTenantsRequest) (*iamv1.ListPlatformTenantsResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	rows, err := h.svc.PlatformTenants(ctx, op.EmployeeID)
	if err != nil {
		return nil, err
	}
	out := make([]*iamv1.PlatformTenant, 0, len(rows))
	for _, r := range rows {
		out = append(out, platformTenantToProto(r))
	}
	return &iamv1.ListPlatformTenantsResponse{Tenants: out}, nil
}

func (h *Handler) CreatePlatformTenant(ctx context.Context, req *iamv1.CreatePlatformTenantRequest) (*iamv1.CreatePlatformTenantResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	ten, inv, err := h.svc.PlatformCreateTenant(ctx, op.EmployeeID, req.GetName(), req.GetAdminEmail())
	if err != nil {
		return nil, err
	}
	return &iamv1.CreatePlatformTenantResponse{
		Tenant:     platformTenantToProto(ten),
		Invitation: invitationToProto(inv),
	}, nil
}

func (h *Handler) ReinvitePlatformAdmin(ctx context.Context, req *iamv1.ReinvitePlatformAdminRequest) (*iamv1.ReinvitePlatformAdminResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	inv, err := h.svc.PlatformReinvite(ctx, op.EmployeeID, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	return &iamv1.ReinvitePlatformAdminResponse{Invitation: invitationToProto(inv)}, nil
}

func (h *Handler) SetPlatformTenantStatus(ctx context.Context, req *iamv1.SetPlatformTenantStatusRequest) (*iamv1.SetPlatformTenantStatusResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.PlatformSetTenantStatus(ctx, op.EmployeeID, grpcx.TenantID(ctx), req.GetTenantId(), req.GetStatus()); err != nil {
		return nil, err
	}
	return &iamv1.SetPlatformTenantStatusResponse{Ok: true}, nil
}

func platformTenantToProto(t app.PlatformTenant) *iamv1.PlatformTenant {
	out := &iamv1.PlatformTenant{
		Id: t.ID, Name: t.Name, Status: t.Status,
		AdminEmail: t.AdminEmail, AdminActivated: t.AdminActivated,
	}
	if !t.CreatedAt.IsZero() {
		out.CreatedAt = t.CreatedAt.Format("2006-01-02 15:04")
	}
	return out
}

func invitationToProto(inv app.Invitation) *iamv1.InviteEmployeeResponse {
	return &iamv1.InviteEmployeeResponse{
		Token: inv.Token, Email: inv.Email, Name: inv.Name,
		ExpiresAt: inv.ExpiresAt.Unix(),
	}
}
