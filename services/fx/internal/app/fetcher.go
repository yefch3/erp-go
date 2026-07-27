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

// RunFetcher fetches once immediately, then on the interval, until ctx ends.
func (s *Service) RunFetcher(ctx context.Context, fetchURL string, symbols []string, interval time.Duration) {
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
