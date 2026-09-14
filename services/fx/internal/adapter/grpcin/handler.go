// Package grpcin adapts gRPC requests onto the app layer.
package grpcin

import (
	"context"
	"time"

	fxv1 "github.com/sgao19/erp-go/gen/go/erp/fx/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/fx/internal/app"
)

type Handler struct {
	fxv1.UnimplementedFxServiceServer
	svc *app.Service
}

func syncStatusProto(v app.SyncStatus) *fxv1.SyncStatus {
	format := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format(time.RFC3339)
	}
	return &fxv1.SyncStatus{State: v.State, LastAttemptAt: format(v.LastAttempt), LastSuccessAt: format(v.LastSuccess), LastError: v.LastError, UsingCache: v.UsingCache, Provider: v.Provider}
}
func (h *Handler) RefreshRates(ctx context.Context, _ *fxv1.RefreshRatesRequest) (*fxv1.RefreshRatesResponse, error) {
	v, err := h.svc.Refresh(ctx)
	return &fxv1.RefreshRatesResponse{Status: syncStatusProto(v)}, err
}
func (h *Handler) GetSyncStatus(context.Context, *fxv1.GetSyncStatusRequest) (*fxv1.GetSyncStatusResponse, error) {
	return &fxv1.GetSyncStatusResponse{Status: syncStatusProto(h.svc.SyncStatus())}, nil
}
func (h *Handler) ListWatchedCurrencies(ctx context.Context, _ *fxv1.ListWatchedCurrenciesRequest) (*fxv1.ListWatchedCurrenciesResponse, error) {
	v, err := h.svc.ListWatched(ctx, grpcx.TenantID(ctx))
	return &fxv1.ListWatchedCurrenciesResponse{Currencies: v}, err
}
func (h *Handler) UpdateWatchedCurrencies(ctx context.Context, r *fxv1.UpdateWatchedCurrenciesRequest) (*fxv1.UpdateWatchedCurrenciesResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	v, err := h.svc.UpdateWatched(ctx, grpcx.TenantID(ctx), op.EmployeeID, r.GetCurrencies())
	return &fxv1.UpdateWatchedCurrenciesResponse{Currencies: v}, err
}

func New(svc *app.Service) *Handler { return &Handler{svc: svc} }

func rateToProto(r app.Rate) *fxv1.Rate {
	return &fxv1.Rate{
		BaseCurrency:  "USD",
		QuoteCurrency: r.QuoteCurrency,
		UnitsPerUsd:   r.UnitsPerUSD.String(),
		UsdPerUnit:    r.USDPerUnit.String(),
		RateDate:      r.RateDate.Format("2006-01-02"),
		Source:        r.Source,
		FetchedAt:     r.FetchedAt.Format(time.RFC3339),
	}
}

func (h *Handler) GetLatestRate(ctx context.Context, req *fxv1.GetLatestRateRequest) (*fxv1.GetLatestRateResponse, error) {
	r, err := h.svc.GetLatest(ctx, req.GetQuoteCurrency())
	if err != nil {
		return nil, err
	}
	return &fxv1.GetLatestRateResponse{Rate: rateToProto(r)}, nil
}

func (h *Handler) ListRates(ctx context.Context, req *fxv1.ListRatesRequest) (*fxv1.ListRatesResponse, error) {
	rates, err := h.svc.ListRates(ctx, req.GetQuoteCurrency(), req.GetDays())
	if err != nil {
		return nil, err
	}
	out := make([]*fxv1.Rate, len(rates))
	for i, r := range rates {
		out[i] = rateToProto(r)
	}
	return &fxv1.ListRatesResponse{Rates: out}, nil
}

func (h *Handler) ListAnomalies(ctx context.Context, _ *fxv1.ListAnomaliesRequest) (*fxv1.ListAnomaliesResponse, error) {
	rows, err := h.svc.ListAnomalies(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*fxv1.Anomaly, len(rows))
	for i, a := range rows {
		out[i] = &fxv1.Anomaly{
			Id: a.ID, QuoteCurrency: a.QuoteCurrency, NewRate: a.NewRate,
			PrevRate: a.PrevRate, DeviationPct: a.DeviationPct, Source: a.Source,
			DetectedAt: a.DetectedAt.Time.Format(time.RFC3339), Note: a.Note,
		}
	}
	return &fxv1.ListAnomaliesResponse{Anomalies: out}, nil
}

func effectiveToProto(r app.EffectiveRate) *fxv1.EffectiveRate {
	return &fxv1.EffectiveRate{BaseCurrency: r.BaseCurrency, QuoteCurrency: r.QuoteCurrency, Rate: r.Rate, ConfirmedBy: r.ConfirmedBy, ConfirmedAt: r.ConfirmedAt, Remark: r.Remark, SystemRate: r.SystemRate, SystemUpdatedAt: r.SystemUpdatedAt}
}
func (h *Handler) ListEffectiveRates(ctx context.Context, _ *fxv1.ListEffectiveRatesRequest) (*fxv1.ListEffectiveRatesResponse, error) {
	rows, err := h.svc.ListEffective(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*fxv1.EffectiveRate, 0, len(rows))
	for _, r := range rows {
		out = append(out, effectiveToProto(r))
	}
	return &fxv1.ListEffectiveRatesResponse{Rates: out}, nil
}
func (h *Handler) ConfirmEffectiveRate(ctx context.Context, req *fxv1.ConfirmEffectiveRateRequest) (*fxv1.ConfirmEffectiveRateResponse, error) {
	r, err := h.svc.ConfirmEffective(ctx, req.GetBaseCurrency(), req.GetQuoteCurrency(), req.GetRate(), req.GetRemark())
	if err != nil {
		return nil, err
	}
	return &fxv1.ConfirmEffectiveRateResponse{Rate: effectiveToProto(r)}, nil
}
