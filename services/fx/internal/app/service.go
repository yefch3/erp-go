// Package app implements the fx use cases: serving latest/historical rates
// and ingesting the API feed, flagging anomalous jumps between fetches.
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

// AnomalyThresholdPct flags fetched rates deviating more than this from the
// previous known rate. The rate is still stored - the flag exists so a human
// reviews it, not to block business.
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
	pool   *pgxpool.Pool
	access Access
	q      *store.Queries
	log    *slog.Logger
}

func New(pool *pgxpool.Pool, log *slog.Logger) *Service {
	return &Service{pool: pool, q: store.New(pool), log: log}
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
