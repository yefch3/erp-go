package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRefreshProviderFailureReturnsDegradedCacheStatus(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer provider.Close()

	svc := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.ConfigureFetcher(provider.URL, []string{"CNY", "EUR"})
	status, err := svc.Refresh(context.Background())
	if err != nil {
		t.Fatalf("provider outage is a supported cache fallback, got %v", err)
	}
	if status.State != "DEGRADED" || !status.UsingCache || status.LastError == "" || status.LastAttempt.IsZero() {
		t.Fatalf("unexpected degraded status: %#v", status)
	}
}

func TestRefreshWithoutFetcherReturnsDegradedStatus(t *testing.T) {
	svc := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	status, err := svc.Refresh(context.Background())
	if err != nil {
		t.Fatalf("missing provider configuration should degrade without breaking the page: %v", err)
	}
	if status.State != "DEGRADED" || !status.UsingCache {
		t.Fatalf("unexpected status: %#v", status)
	}
}
