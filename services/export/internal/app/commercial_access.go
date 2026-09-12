package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"time"
)

func (s *Service) RequireAnyPermission(ctx context.Context, codes ...string) error {
	op, ok := grpcx.OperatorFromContext(ctx)
	if !ok || op.EmployeeID <= 0 || op.TenantID <= 0 {
		return apierr.Unauthorized("EX_ACTOR_REQUIRED", "请先登录")
	}
	access, ok := s.scopes.(OfferAccess)
	if !ok {
		return apierr.Permission("EX_ACCESS_REQUIRED", "权限校验不可用")
	}
	for _, code := range codes {
		allowed, err := access.HasPermission(ctx, op.EmployeeID, code)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
	}
	return apierr.Permission("EX_COMMERCIAL_ACCESS", "没有查看对客合同价格或收款信息的权限")
}

func (s *Service) effectiveContractRate(ctx context.Context, currency string) (Rate, error) {
	provider, ok := s.rates.(OfferRates)
	if !ok {
		return Rate{}, apierr.Invalid("OFFER_FX_REQUIRED", "有效汇率服务不可用")
	}
	rates, err := provider.EffectiveRates(ctx)
	if err != nil {
		return Rate{}, err
	}
	value, at, err := effectivePair(rates, "USD", currency)
	if err != nil {
		return Rate{}, err
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return Rate{Rate: value, Base: "USD", Source: "MANUAL_EFFECTIVE", At: at}, nil
}
