package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

type effectiveTestAccess struct{}

func (effectiveTestAccess) HasPermission(_ context.Context, id int64, code string) (bool, error) {
	return id == 1 || (id == 2 && code == "export:quotation:read"), nil
}
func TestEffectiveRateValidation(t *testing.T) {
	for _, value := range []string{"0", "-1", "NaN", "1e2", "0.000000001", "99999999999999999"} {
		if _, err := validateEffective("USD", "CNY", value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	if _, err := validateEffective("USD", "USD", "1"); err == nil {
		t.Error("accepted same currency")
	}
	if v, err := validateEffective("USD", "CNY", "7.24000001"); err != nil || v.String() != "7.24000001" {
		t.Fatalf("lost rate precision: %v %v", v, err)
	}
}
func TestEffectiveRatesIsolationAndPermissions(t *testing.T) {
	dsn := os.Getenv("FX_TEST_DSN")
	if dsn == "" {
		t.Skip("FX_TEST_DSN is required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.SetAccess(effectiveTestAccess{})
	tenant := time.Now().UnixNano()%1000000000 + 1000000000
	actor := func(company, id int64) context.Context {
		return grpcx.WithOperator(ctx, grpcx.Operator{TenantID: company, EmployeeID: id, Name: "D2 tester"})
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM effective_rates WHERE tenant_id=$1 OR tenant_id=$2", tenant, tenant+1)
	})
	if _, err = svc.ConfirmEffective(ctx, "USD", "CNY", "7.2", ""); err == nil {
		t.Fatal("anonymous write allowed")
	}
	if _, err = svc.ConfirmEffective(actor(tenant, 2), "USD", "CNY", "7.2", ""); err == nil {
		t.Fatal("sales write allowed")
	}
	first, err := svc.ConfirmEffective(actor(tenant, 1), "usd", "cny", "7.2", "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ConfirmEffective(actor(tenant+1, 1), "USD", "CNY", "8.1", "other company"); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ConfirmEffective(actor(tenant, 1), "USD", "CNY", "7.3", "second"); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ListEffective(actor(tenant, 2))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.BaseCurrency == "USD" && r.QuoteCurrency == "CNY" {
			found = true
			if !strings.HasPrefix(r.Rate, "7.3") || r.Remark != "second" || r.ConfirmedBy != "D2 tester" {
				t.Fatalf("wrong effective row: %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("effective rate missing")
	}
	if first.Rate != "7.2" {
		t.Fatal("returned snapshot changed")
	}
	rows, err = svc.ListEffective(actor(tenant+1, 2))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.BaseCurrency == "USD" && r.QuoteCurrency == "CNY" && !strings.HasPrefix(r.Rate, "8.1") {
			t.Fatal("cross-tenant overwrite")
		}
	}
	if _, err = svc.ListEffective(actor(tenant, 3)); err == nil {
		t.Fatal("unauthorized read allowed")
	}
}
