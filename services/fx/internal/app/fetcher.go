package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/services/fx/internal/store"
)

// Fetch pulls the latest reference rates and upserts one row per symbol.
// A failed fetch only logs: the system keeps serving the last known or
// manual rates, which is the designed fallback.
func (s *Service) Fetch(ctx context.Context, fetchURL string, symbols []string) error {
	u, err := url.Parse(fetchURL)
	if err != nil {
		return fmt.Errorf("fx: bad fetch url: %w", err)
	}
	q := u.Query()
	q.Set("base", "USD")
	q.Set("symbols", strings.Join(symbols, ","))
	u.RawQuery = q.Encode()

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fx: fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fx: fetch: unexpected status %d", resp.StatusCode)
	}

	var body struct {
		Base  string             `json:"base"`
		Date  string             `json:"date"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("fx: decode: %w", err)
	}
	rateDate, err := time.Parse("2006-01-02", body.Date)
	if err != nil {
		return fmt.Errorf("fx: bad date %q: %w", body.Date, err)
	}

	for symbol, v := range body.Rates {
		// The feed hands us float64; converting via string keeps the exact
		// published digits instead of binary noise.
		rate := decimal.NewFromFloat(v)
		s.flagIfAnomalous(ctx, symbol, rate)
		if err := s.q.UpsertRate(ctx, store.UpsertRateParams{
			QuoteCurrency: symbol, Rate: rate.String(),
			RateDate: pgtype.Date{Time: rateDate, Valid: true},
			Source:   "FRANKFURTER", Note: "auto fetch",
		}); err != nil {
			return fmt.Errorf("fx: upsert %s: %w", symbol, err)
		}
	}
	s.log.Info("fx: rates fetched", "date", body.Date, "symbols", len(body.Rates))
	return nil
}

// BackfillHistory pulls a window of daily rates in one request, so a
// freshly deployed system has a curve to draw instead of a single dot.
//
// The scheduled fetch only ever asks for today, which means history accrues
// one day per day: a system deployed this morning can show a chart with one
// point on it, and would need half a year of uptime before the 汇率走势 chart
// said anything. The feed publishes its whole series for free, so there is no
// reason to make anybody wait for it.
//
// Runs on every start rather than once: the upsert is idempotent, and a
// service that was down for a fortnight comes back with the fortnight it
// missed. One request and a few hundred upserts, at a moment when nobody is
// waiting.
//
// Deliberately does NOT run anomaly detection. flagIfAnomalous compares
// against the latest stored rate, and feeding it a year of history in
// ascending order would either flood the anomaly list with ordinary
// week-to-week drift or, worse, compare an old rate against a newer one and
// invent jumps that never happened.
func (s *Service) BackfillHistory(ctx context.Context, fetchURL string, symbols []string, days int) error {
	if days <= 0 {
		return nil
	}
	// The configured URL points at /latest; the series lives at the same host
	// under a date range. Derived rather than configured separately so the two
	// cannot end up pointing at different feeds.
	base := strings.TrimSuffix(strings.TrimSuffix(fetchURL, "/"), "/latest")
	end := time.Now().UTC()
	start := end.AddDate(0, 0, -days)
	u, err := url.Parse(fmt.Sprintf("%s/%s..%s", base,
		start.Format("2006-01-02"), end.Format("2006-01-02")))
	if err != nil {
		return fmt.Errorf("fx: bad backfill url: %w", err)
	}
	q := u.Query()
	q.Set("base", "USD")
	q.Set("symbols", strings.Join(symbols, ","))
	u.RawQuery = q.Encode()

	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fx: backfill: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fx: backfill: unexpected status %d", resp.StatusCode)
	}
	var body struct {
		// One entry per published day, each a map of symbol to rate. Working
		// days only — the feed republishes ECB reference rates.
		Rates map[string]map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("fx: backfill decode: %w", err)
	}

	stored := 0
	for day, rates := range body.Rates {
		rateDate, err := time.Parse("2006-01-02", day)
		if err != nil {
			// One malformed key must not cost the other two hundred days.
			s.log.Warn("fx: backfill: skipping unparseable date", "date", day)
			continue
		}
		for symbol, v := range rates {
			if err := s.q.UpsertRate(ctx, store.UpsertRateParams{
				QuoteCurrency: symbol, Rate: decimal.NewFromFloat(v).String(),
				RateDate: pgtype.Date{Time: rateDate, Valid: true},
				Source:   "FRANKFURTER", Note: "history backfill",
			}); err != nil {
				return fmt.Errorf("fx: backfill upsert %s %s: %w", symbol, day, err)
			}
			stored++
		}
	}
	s.log.Info("fx: history backfilled", "days", days, "rows", stored)
	return nil
}

// RunFetcher backfills history once, then fetches today's rates immediately
// and on the interval, until ctx ends.
func (s *Service) RunFetcher(ctx context.Context, fetchURL string, symbols []string, interval time.Duration, backfillDays int) {
	// Before the first fetch, so the anomaly comparison that Fetch performs
	// has real yesterday to compare against rather than nothing.
	if err := s.BackfillHistory(ctx, fetchURL, symbols, backfillDays); err != nil {
		s.log.Warn("fx: history backfill failed; charts will fill in day by day", "err", err)
	}
	if err := s.Fetch(ctx, fetchURL, symbols); err != nil {
		s.log.Warn("fx: initial fetch failed; serving stored/manual rates", "err", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Fetch(ctx, fetchURL, symbols); err != nil {
				s.log.Warn("fx: scheduled fetch failed", "err", err)
			}
		}
	}
}

// flagIfAnomalous records an anomaly when the incoming rate jumps more than
// the threshold from the last stored rate. Detection lives on the fetch path
// now that the feed is the only source.
func (s *Service) flagIfAnomalous(ctx context.Context, symbol string, newRate decimal.Decimal) {
	prev, err := s.q.LatestRate(ctx, symbol)
	if err != nil {
		return // first rate for this symbol: nothing to compare
	}
	prevRate, err := decimal.NewFromString(prev.Rate)
	if err != nil || prevRate.IsZero() || prevRate.Equal(newRate) {
		return
	}
	deviation := newRate.Sub(prevRate).Abs().Div(prevRate).Mul(decimal.NewFromInt(100)).Round(4)
	if deviation.LessThanOrEqual(decimal.NewFromFloat(AnomalyThresholdPct)) {
		return
	}
	if err := s.q.InsertAnomaly(ctx, store.InsertAnomalyParams{
		QuoteCurrency: symbol, NewRate: newRate.String(), PrevRate: prevRate.String(),
		DeviationPct: deviation.String(), Source: "FRANKFURTER",
		Note: "auto fetch vs " + prev.RateDate.Time.Format("2006-01-02"),
	}); err != nil {
		s.log.Warn("fx: record anomaly failed", "symbol", symbol, "err", err)
	}
	s.log.Warn("fx: anomalous rate fetched", "symbol", symbol,
		"new", newRate.String(), "prev", prevRate.String(), "deviation_pct", deviation.String())
}
