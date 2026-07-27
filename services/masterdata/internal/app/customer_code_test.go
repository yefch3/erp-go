package app

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// TestCreateCustomerIssuesCode covers the two halves of save-time numbering:
// an empty code is filled in by the service, and a create that fails after
// drawing a number leaves the sequence where it was. Needs a real PostgreSQL
// with migrations applied; set MD_TEST_DSN to run it.
func TestCreateCustomerIssuesCode(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set; skipping DB-backed test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool)

	created, _, err := svc.CreateCustomer(ctx, 1, CustomerInput{Name: "Numbering Probe"})
	if err != nil {
		t.Fatalf("create with empty code: %v", err)
	}
	defer deleteCustomer(ctx, t, pool, created.ID)
	if created.Code == "" {
		t.Fatal("expected an issued code, got empty")
	}

	// Arrange a collision: park the code that the next create would draw.
	seqBefore := customerSeq(ctx, t, pool)
	squatted := fmt.Sprintf("CU-%04d", seqBefore+1)
	var squatterID int64
	if err := pool.QueryRow(ctx,
		"INSERT INTO customers (tenant_id, code, name, currency) VALUES (1, $1, 'Squatter', 'USD') RETURNING id",
		squatted,
	).Scan(&squatterID); err != nil {
		t.Fatalf("park code %s: %v", squatted, err)
	}
	defer deleteCustomer(ctx, t, pool, squatterID)

	if _, _, err := svc.CreateCustomer(ctx, 1, CustomerInput{Name: "Collision Probe"}); err == nil {
		t.Fatalf("expected create to fail on the parked code %s", squatted)
	}

	// The drawn number rolled back with the insert, so nothing was burned.
	if got := customerSeq(ctx, t, pool); got != seqBefore {
		t.Fatalf("failed create moved the sequence: %d -> %d", seqBefore, got)
	}
}

func customerSeq(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var seq int64
	if err := pool.QueryRow(ctx,
		"SELECT next_seq FROM number_sequences WHERE tenant_id = 1 AND biz_type = 'CUSTOMER'",
	).Scan(&seq); err != nil {
		t.Fatalf("read sequence: %v", err)
	}
	return seq
}

func deleteCustomer(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id int64) {
	t.Helper()
	if _, err := pool.Exec(ctx, "DELETE FROM customers WHERE id = $1", id); err != nil {
		t.Logf("cleanup customer %d: %v", id, err)
	}
}
