package app

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestPeriodKeyFor(t *testing.T) {
	at := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	cases := map[string]string{
		"DAILY": "20260727", "MONTHLY": "202607", "YEARLY": "2026", "NONE": "",
	}
	for period, want := range cases {
		if got := periodKeyFor(period, at); got != want {
			t.Fatalf("periodKeyFor(%s) = %q, want %q", period, got, want)
		}
	}
}

// TestNextNumberConcurrency is the acceptance test for the numbering
// service: 100 concurrent callers must receive 100 distinct numbers.
// It needs a real PostgreSQL with migrations applied; set MD_TEST_DSN to
// run it, e.g.
//
//	MD_TEST_DSN='postgres://erp_masterdata:erp_masterdata_pw@localhost:5433/erp_masterdata?sslmode=disable' go test ./internal/app/
func TestNextNumberConcurrency(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set; skipping DB-backed concurrency test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool)

	const n = 100
	var wg sync.WaitGroup
	results := make(chan string, n)
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			num, err := svc.NextNumber(ctx, 1, "QUOTATION")
			if err != nil {
				errs <- err
				return
			}
			results <- num
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("NextNumber failed under concurrency: %v", err)
	}
	seen := make(map[string]bool, n)
	for num := range results {
		if seen[num] {
			t.Fatalf("duplicate number issued: %s", num)
		}
		seen[num] = true
	}
	if len(seen) != n {
		t.Fatalf("expected %d distinct numbers, got %d", n, len(seen))
	}
	t.Logf("100 concurrent calls -> 100 distinct numbers, sample: %s", firstKey(seen))
}

func firstKey(m map[string]bool) string {
	for k := range m {
		return k
	}
	return ""
}
