package app

import (
	"context"
	"os"
	"testing"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// TestDeactivateSupplierSuspendsFactories 验证供应商停用、工厂联动暂停和重新启用不自动恢复。
func TestDeactivateSupplierSuspendsFactories(t *testing.T) {
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

	supplier, err := svc.CreateSupplier(ctx, 1, SupplierInput{
		NameZh: "供应商联动测试", CountryCode: "CN", BusinessTypes: []string{"GENERAL"},
		OperatorID: 1, OperatorName: "test",
	})
	if err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	factory, err := svc.CreateFactory(ctx, 1, FactoryInput{
		SupplierID: supplier.ID, NameZh: "工厂联动测试", CountryCode: "CN",
		Timezone: "Asia/Shanghai", Status: "COOPERATING", OperatorID: 1, OperatorName: "test",
	})
	if err != nil {
		t.Fatalf("create factory: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM factory_change_logs WHERE tenant_id=1 AND factory_id=$1", factory.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM factories WHERE tenant_id=1 AND id=$1", factory.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM supplier_change_logs WHERE tenant_id=1 AND supplier_id=$1", supplier.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM suppliers WHERE tenant_id=1 AND id=$1", supplier.ID)
	}()

	if err := svc.DeactivateSupplier(ctx, 1, supplier.ID, 1, "test"); err != nil {
		t.Fatalf("deactivate supplier: %v", err)
	}
	paused, err := svc.GetFactory(ctx, 1, factory.ID)
	if err != nil || paused.Status != "SUSPENDED" {
		t.Fatalf("factory status=%q err=%v, want SUSPENDED", paused.Status, err)
	}

	paused.Status = "COOPERATING"
	if _, err = svc.UpdateFactory(ctx, 1, factory.ID, FactoryInput{
		SupplierID: paused.SupplierID, NameZh: paused.NameZh, NameEn: paused.NameEn,
		CountryCode: paused.CountryCode, Timezone: paused.Timezone, Status: paused.Status,
		OperatorID: 1, OperatorName: "test",
	}); err == nil {
		t.Fatal("inactive supplier must prevent factory reactivation")
	}

	if err = svc.ActivateSupplier(ctx, 1, supplier.ID, 1); err != nil {
		t.Fatalf("activate supplier: %v", err)
	}
	stillPaused, err := svc.GetFactory(ctx, 1, factory.ID)
	if err != nil || stillPaused.Status != "SUSPENDED" {
		t.Fatalf("factory status after supplier activation=%q err=%v, want SUSPENDED", stillPaused.Status, err)
	}
}
