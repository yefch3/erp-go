package app

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/shopspring/decimal"
)

type Access interface {
	HasPermission(context.Context, int64, string) (bool, error)
}

func (s *Service) SetAccess(access Access) { s.access = access }

type EffectiveRate struct {
	BaseCurrency, QuoteCurrency, Rate string
	ConfirmedBy, ConfirmedAt, Remark  string
	SystemRate, SystemUpdatedAt       string
}

func (s *Service) effectiveAccess(ctx context.Context, write bool) (grpcx.Operator, error) {
	op, ok := grpcx.OperatorFromContext(ctx)
	if !ok || op.TenantID <= 0 || op.EmployeeID <= 0 {
		return op, apierr.Unauthorized("FX_ACTOR_REQUIRED", "请先登录")
	}
	if s.access == nil {
		return op, apierr.Permission("FX_ACCESS_UNAVAILABLE", "汇率权限校验不可用")
	}
	codes := []string{"fx:rate:read", "export:quotation:read"}
	if write {
		codes = []string{"fx:rate:write"}
	}
	for _, code := range codes {
		allowed, err := s.access.HasPermission(ctx, op.EmployeeID, code)
		if err != nil {
			return op, err
		}
		if allowed {
			return op, nil
		}
	}
	return op, apierr.Permission("FX_PERMISSION", "没有操作有效汇率的权限")
}

var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)
var effectiveDecimal = regexp.MustCompile(`^[0-9]{1,16}(\.[0-9]{1,8})?$`)

func validateEffective(base, quote, value string) (decimal.Decimal, error) {
	if !currencyCode.MatchString(base) || !currencyCode.MatchString(quote) || base == quote {
		return decimal.Zero, apierr.Invalid("FX_PAIR_INVALID", "请选择两个不同的三位币种代码")
	}
	v, err := decimal.NewFromString(value)
	if err != nil || !effectiveDecimal.MatchString(value) || !v.IsPositive() {
		return decimal.Zero, apierr.Invalid("FX_EFFECTIVE_INVALID", "有效汇率必须为正数，最多保留八位小数")
	}
	return v, nil
}

func (s *Service) ConfirmEffective(ctx context.Context, base, quote, value, remark string) (EffectiveRate, error) {
	op, err := s.effectiveAccess(ctx, true)
	if err != nil {
		return EffectiveRate{}, err
	}
	base, quote, value = strings.ToUpper(strings.TrimSpace(base)), strings.ToUpper(strings.TrimSpace(quote)), strings.TrimSpace(value)
	v, err := validateEffective(base, quote, value)
	if err != nil {
		return EffectiveRate{}, err
	}
	if len([]rune(remark)) > 2000 {
		return EffectiveRate{}, apierr.Invalid("FX_REMARK_LONG", "备注最多 2000 字")
	}
	var at time.Time
	err = s.pool.QueryRow(ctx, `INSERT INTO effective_rates
		(tenant_id,base_currency,quote_currency,rate,confirmed_by_id,confirmed_by,remark)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT(tenant_id,base_currency,quote_currency) DO UPDATE SET
		rate=EXCLUDED.rate,confirmed_by_id=EXCLUDED.confirmed_by_id,confirmed_by=EXCLUDED.confirmed_by,
		remark=EXCLUDED.remark,confirmed_at=clock_timestamp() RETURNING confirmed_at`,
		op.TenantID, base, quote, v.String(), op.EmployeeID, op.Name, strings.TrimSpace(remark)).Scan(&at)
	if err != nil {
		return EffectiveRate{}, err
	}
	return EffectiveRate{BaseCurrency: base, QuoteCurrency: quote, Rate: v.String(), ConfirmedBy: op.Name, ConfirmedAt: at.Format(time.RFC3339Nano), Remark: strings.TrimSpace(remark)}, nil
}

func (s *Service) ListEffective(ctx context.Context) ([]EffectiveRate, error) {
	op, err := s.effectiveAccess(ctx, false)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `WITH pairs AS (
		SELECT base_currency::text AS base,quote_currency::text AS quote FROM fx_rates
		UNION SELECT base_currency,quote_currency FROM effective_rates WHERE tenant_id=$1
	)
	SELECT p.base,p.quote,COALESCE(e.rate::text,''),COALESCE(e.confirmed_by,''),
	COALESCE(to_char(e.confirmed_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),''),COALESCE(e.remark,''),
	COALESCE((CASE WHEN p.base='USD' THEN q.rate WHEN p.quote='USD' THEN 1/b.rate ELSE q.rate/b.rate END)::text,''),
	COALESCE(to_char(LEAST(q.fetched_at,b.fetched_at) AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'')
	FROM pairs p
	LEFT JOIN effective_rates e ON e.tenant_id=$1 AND e.base_currency=p.base AND e.quote_currency=p.quote
	LEFT JOIN LATERAL (SELECT rate,fetched_at FROM fx_rates WHERE quote_currency=p.quote ORDER BY rate_date DESC,fetched_at DESC LIMIT 1) q ON true
	LEFT JOIN LATERAL (SELECT rate,fetched_at FROM fx_rates WHERE quote_currency=p.base ORDER BY rate_date DESC,fetched_at DESC LIMIT 1) b ON true
	ORDER BY p.base,p.quote`, op.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EffectiveRate{}
	for rows.Next() {
		var r EffectiveRate
		if err := rows.Scan(&r.BaseCurrency, &r.QuoteCurrency, &r.Rate, &r.ConfirmedBy, &r.ConfirmedAt, &r.Remark, &r.SystemRate, &r.SystemUpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
