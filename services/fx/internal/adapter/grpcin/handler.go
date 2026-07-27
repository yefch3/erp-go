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

func (h *Handler) SetManualRate(ctx context.Context, req *fxv1.SetManualRateRequest) (*fxv1.SetManualRateResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	stored, anomaly, deviation, err := h.svc.SetManual(ctx,
		req.GetQuoteCurrency(), req.GetUnitsPerUsd(), op.EmployeeID, req.GetNote())
	if err != nil {
		return nil, err
	}
	return &fxv1.SetManualRateResponse{
		Rate: rateToProto(stored), Anomaly: anomaly, DeviationPct: deviation.String(),
	}, nil
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
