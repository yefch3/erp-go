// Package app implements the fx use cases: serving latest/historical rates,
// accepting manual quotes with anomaly detection, and ingesting the API feed.
package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/fx/internal/store"
)

// AnomalyThresholdPct flags manual or fetched rates deviating more than this
// from the previous known rate. The rate is still accepted - the flag exists
// so a human reviews it, not to block business.
const AnomalyThresholdPct = 2.0

type Rate struct {
	QuoteCurrency string
	UnitsPerUSD   decimal.Decimal
	USDPerUnit    decimal.Decimal
	RateDate      time.Time
	Source        string
	FetchedAt     time.Time
}

type Service struct {
	q   *store.Queries
	log *slog.Logger
}

func New(pool *pgxpool.Pool, log *slog.Logger) *Service {
	return &Service{q: store.New(pool), log: log}
}

func (s *Service) GetLatest(ctx context.Context, quote string) (Rate, error) {
	if err := validCurrency(quote); err != nil {
		return Rate{}, err
	}
	row, err := s.q.LatestRate(ctx, quote)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Rate{}, apierr.NotFound("FX_RATE_NOT_FOUND", "该币种暂无汇率数据").
				WithMeta("currency", quote)
		}
		return Rate{}, err
	}
	return rowToRate(row.QuoteCurrency, row.Rate, row.RateDate, row.Source, row.FetchedAt)
}

func (s *Service) ListRates(ctx context.Context, quote string, days int32) ([]Rate, error) {
	if err := validCurrency(quote); err != nil {
		return nil, err
	}
	if days <= 0 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -int(days))
	rows, err := s.q.ListRates(ctx, store.ListRatesParams{
		QuoteCurrency: quote,
		RateDate:      pgtype.Date{Time: since, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	out := make([]Rate, 0, len(rows))
	for _, r := range rows {
		rate, err := rowToRate(r.QuoteCurrency, r.Rate, r.RateDate, r.Source, r.FetchedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, rate)
	}
	return out, nil
}

// SetManual records a finance-entered rate for today. Returns the stored
// rate plus whether it deviated anomalously from the previous known rate.
func (s *Service) SetManual(ctx context.Context, quote, unitsPerUSD string, operatorID int64, note string) (Rate, bool, decimal.Decimal, error) {
	if err := validCurrency(quote); err != nil {
		return Rate{}, false, decimal.Zero, err
	}
	newRate, err := decimal.NewFromString(unitsPerUSD)
	if err != nil || newRate.LessThanOrEqual(decimal.Zero) {
		return Rate{}, false, decimal.Zero, apierr.Invalid("FX_RATE_INVALID", "汇率必须是正数")
	}

	anomaly, deviation := false, decimal.Zero
	if prev, err := s.q.LatestRate(ctx, quote); err == nil {
		prevRate, perr := decimal.NewFromString(prev.Rate)
		if perr == nil && !prevRate.IsZero() {
			deviation = newRate.Sub(prevRate).Abs().Div(prevRate).Mul(decimal.NewFromInt(100)).Round(4)
			if deviation.GreaterThan(decimal.NewFromFloat(AnomalyThresholdPct)) {
				anomaly = true
				if err := s.q.InsertAnomaly(ctx, store.InsertAnomalyParams{
					QuoteCurrency: quote, NewRate: newRate.String(), PrevRate: prevRate.String(),
					DeviationPct: deviation.String(), Source: "MANUAL", Note: note,
				}); err != nil {
					return Rate{}, false, decimal.Zero, err
				}
			}
		}
	}

	today := pgtype.Date{Time: time.Now(), Valid: true}
	if err := s.q.UpsertRate(ctx, store.UpsertRateParams{
		QuoteCurrency: quote, Rate: newRate.String(), RateDate: today,
		Source: "MANUAL", CreatedBy: operatorID, Note: note,
	}); err != nil {
		return Rate{}, false, decimal.Zero, err
	}
	stored, err := s.GetLatest(ctx, quote)
	return stored, anomaly, deviation, err
}

type AnomalyRow = store.ListAnomaliesRow

func (s *Service) ListAnomalies(ctx context.Context) ([]AnomalyRow, error) {
	return s.q.ListAnomalies(ctx)
}

func rowToRate(quote, rateStr string, date pgtype.Date, source string, fetched pgtype.Timestamptz) (Rate, error) {
	units, err := decimal.NewFromString(rateStr)
	if err != nil {
		return Rate{}, err
	}
	return Rate{
		QuoteCurrency: quote,
		UnitsPerUSD:   units,
		USDPerUnit:    decimal.NewFromInt(1).Div(units).Round(8),
		RateDate:      date.Time,
		Source:        source,
		FetchedAt:     fetched.Time,
	}, nil
}

func validCurrency(c string) error {
	if len(c) != 3 {
		return apierr.Invalid("FX_CURRENCY_INVALID", "币种必须是 3 位 ISO 代码")
	}
	return nil
}
