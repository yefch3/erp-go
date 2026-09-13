// Package app implements the fx use cases: serving latest/historical rates
// and ingesting the API feed, flagging anomalous jumps between fetches.
package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
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
	pool                     *pgxpool.Pool
	access                   Access
	q                        *store.Queries
	log                      *slog.Logger
	syncMu                   sync.RWMutex
	fetchURL                 string
	fetchSymbols             []string
	lastAttempt, lastSuccess time.Time
	lastFetchError           string
}

func New(pool *pgxpool.Pool, log *slog.Logger) *Service {
	return &Service{pool: pool, q: store.New(pool), log: log}
}

type SyncStatus struct {
	State                    string
	LastAttempt, LastSuccess time.Time
	LastError                string
	UsingCache               bool
	Provider                 string
}

func (s *Service) ConfigureFetcher(url string, symbols []string) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	s.fetchURL = url
	s.fetchSymbols = append([]string(nil), symbols...)
}
func (s *Service) Refresh(ctx context.Context) (SyncStatus, error) {
	s.syncMu.RLock()
	url := s.fetchURL
	symbols := append([]string(nil), s.fetchSymbols...)
	s.syncMu.RUnlock()
	if url == "" {
		return s.SyncStatus(), apierr.Internal("FX_FETCH_NOT_CONFIGURED", "汇率同步尚未配置")
	}
	err := s.Fetch(ctx, url, symbols)
	return s.SyncStatus(), err
}
func (s *Service) SyncStatus() SyncStatus {
	s.syncMu.RLock()
	defer s.syncMu.RUnlock()
	state := "READY"
	if s.lastFetchError != "" {
		state = "DEGRADED"
	}
	if s.lastAttempt.IsZero() {
		state = "STARTING"
	}
	return SyncStatus{State: state, LastAttempt: s.lastAttempt, LastSuccess: s.lastSuccess, LastError: s.lastFetchError, UsingCache: s.lastFetchError != "", Provider: "Frankfurter / ECB"}
}
func (s *Service) recordFetch(err error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	s.lastAttempt = time.Now().UTC()
	if err != nil {
		s.lastFetchError = err.Error()
	} else {
		s.lastFetchError = ""
		s.lastSuccess = s.lastAttempt
	}
}
func (s *Service) ListWatched(ctx context.Context, tenantID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT currency FROM fx_watch_currencies WHERE tenant_id=$1 ORDER BY sort_order,currency`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return []string{"CNY", "EUR", "GBP", "JPY", "HKD"}, nil
	}
	return out, rows.Err()
}
func (s *Service) UpdateWatched(ctx context.Context, tenantID, actorID int64, currencies []string) ([]string, error) {
	clean := []string{}
	seen := map[string]bool{}
	for _, c := range currencies {
		c = strings.ToUpper(strings.TrimSpace(c))
		if err := validCurrency(c); err != nil {
			return nil, err
		}
		if !seen[c] && c != "USD" {
			seen[c] = true
			clean = append(clean, c)
		}
	}
	if len(clean) == 0 || len(clean) > 12 {
		return nil, apierr.Invalid("FX_WATCH_INVALID", "请关注 1 至 12 个非 USD 币种")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM fx_watch_currencies WHERE tenant_id=$1`, tenantID); err != nil {
		return nil, err
	}
	for i, c := range clean {
		if _, err = tx.Exec(ctx, `INSERT INTO fx_watch_currencies(tenant_id,currency,sort_order,created_by) VALUES($1,$2,$3,$4)`, tenantID, c, i, actorID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return clean, nil
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
