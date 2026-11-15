package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"os"
	"testing"
	"time"
)

func TestContractReceiptSettlementStatusUsesFinanceConfirmation(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN required")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"contract_receivable_closures", "contract_items", "contract_versions", "contracts"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant)
		}
	}()
	id := seedContract(ctx, t, pool, tenant, "CT-STATUS", "USD", "100")
	svc := New(pool, Deps{})
	state, err := svc.ContractReceiptSettlementStatus(ctx, tenant, id)
	if err != nil || state != "PENDING" {
		t.Fatal(state, err)
	}
	if err = svc.CloseReceivable(ctx, tenant, id, "SETTLED", "Independent fixture", Operator{ID: 5, Name: "Finance"}); err != nil {
		t.Fatal(err)
	}
	state, err = svc.ContractReceiptSettlementStatus(ctx, tenant, id)
	if err != nil || state != "SETTLED" {
		t.Fatal(state, err)
	}
	foreign, err := svc.ContractReceiptSettlementStatus(ctx, tenant+1, id)
	if err != nil || foreign != "PENDING" {
		t.Fatal("tenant leak", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE contract_receivable_closures SET revoked_at=now() WHERE tenant_id=$1 AND contract_id=$2`, tenant, id); err != nil {
		t.Fatal(err)
	}
	state, err = svc.ContractReceiptSettlementStatus(ctx, tenant, id)
	if err != nil || state != "PENDING" {
		t.Fatal("reopened account still settled", state, err)
	}
}
