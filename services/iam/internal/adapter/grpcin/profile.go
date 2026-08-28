package grpcin

import (
	"context"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/iam/internal/app"
)

// 「我的资料」的入口。
//
// 三个方法都不从请求里取员工 id——身份一律来自 grpcx.OperatorFromContext，
// 也就是网关验过的那张令牌。头像那两个是例外，因为管理员也要能替别人换照片；
// 那时 employee_id 非零，而「能不能替别人改」由网关的权限中间件把关，
// 不在这一层重复判断（重复判断会变成两套规则，早晚对不上）。

func (h *Handler) GetMyProfile(ctx context.Context, _ *iamv1.GetMyProfileRequest) (*iamv1.GetMyProfileResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	view, err := h.svc.GetMyProfile(ctx, grpcx.TenantID(ctx), op.EmployeeID)
	if err != nil {
		return nil, err
	}
	return &iamv1.GetMyProfileResponse{Profile: myProfileToProto(view)}, nil
}

func (h *Handler) UpdateMyProfile(ctx context.Context, req *iamv1.UpdateMyProfileRequest) (*iamv1.UpdateMyProfileResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	view, err := h.svc.UpdateMyProfile(ctx, grpcx.TenantID(ctx), op.EmployeeID, app.UpdateMyProfileInput{
		EnglishName:     req.GetEnglishName(),
		Phone:           req.GetPhone(),
		ExpectedVersion: req.GetExpectedVersion(),
	})
	if err != nil {
		return nil, err
	}
	return &iamv1.UpdateMyProfileResponse{Profile: myProfileToProto(view)}, nil
}

func (h *Handler) PresignAvatarUpload(ctx context.Context, req *iamv1.PresignAvatarUploadRequest) (*iamv1.PresignAvatarUploadResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	key, url, expires, err := h.svc.PresignAvatarUpload(ctx, grpcx.TenantID(ctx),
		targetEmployee(req.GetEmployeeId(), op.EmployeeID), req.GetFileName(), req.GetContentType())
	if err != nil {
		return nil, err
	}
	return &iamv1.PresignAvatarUploadResponse{
		FileKey: key, UploadUrl: url, ExpiresInSeconds: expires,
	}, nil
}

func (h *Handler) SetAvatar(ctx context.Context, req *iamv1.SetAvatarRequest) (*iamv1.SetAvatarResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	key, err := h.svc.SetAvatar(ctx, grpcx.TenantID(ctx),
		targetEmployee(req.GetEmployeeId(), op.EmployeeID), op.EmployeeID, req.GetFileKey())
	if err != nil {
		return nil, err
	}
	return &iamv1.SetAvatarResponse{AvatarKey: key}, nil
}

func (h *Handler) AvatarURLs(ctx context.Context, req *iamv1.AvatarURLsRequest) (*iamv1.AvatarURLsResponse, error) {
	urls, err := h.svc.AvatarURLs(ctx, grpcx.TenantID(ctx), req.GetEmployeeIds())
	if err != nil {
		return nil, err
	}
	return &iamv1.AvatarURLsResponse{Urls: urls}, nil
}

// targetEmployee：0 表示「我自己」。让调用方省掉自己填 id 这一步，
// 顺带堵住一类低级错误——前端拿不到自己的 id 时填了 0，结果去改了 0 号员工。
func targetEmployee(requested, self int64) int64 {
	if requested == 0 {
		return self
	}
	return requested
}

func myProfileToProto(v app.MyProfileView) *iamv1.MyProfile {
	return &iamv1.MyProfile{
		Id: v.ID, Code: v.Code, Name: v.Name, EnglishName: v.EnglishName,
		DepartmentName: v.DepartmentName, Position: v.Position,
		Email: v.Email, Phone: v.Phone, Status: v.Status,
		ManagerName: v.ManagerName, HireDate: v.HireDate,
		AvatarKey: v.AvatarKey, AvatarUrl: v.AvatarURL,
		Version: v.Version, EmailVerified: v.EmailVerified,
	}
}
