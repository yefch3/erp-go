package app

import (
	"sync"
	"testing"
)

func TestHistoricalLogisticsAccountUniqueAndPayable(t *testing.T) {
	ctx, svc, tenant, cleanup := reconTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	input := ManualPayableInput{BusinessType: "LOGISTICS", SourceBusinessID: 9901, SourceSupplierID: 8, SupplierName: "Historical forwarder", OrderNo: "LO-HISTORY", Currency: "USD", TotalAmount: "1000", PaidAmount: "100", PaidAt: "2026-11-15", PaymentTerms: "Net30"}
	var wg sync.WaitGroup
	results := make(chan SupplierReconRow, 6)
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			row, err := svc.CreateManualPayable(ctx, tenant, input, op)
			if err != nil {
				errs <- err
			} else {
				results <- row
			}
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	if len(results) != 1 || len(errs) != 5 {
		for err := range errs {
			t.Log(err)
		}
		t.Fatalf("concurrent create: %d successes", len(results))
	}
	for err := range errs {
		wantAllocErr(t, err, "PR_LOGISTICS_ACCOUNT_EXISTS")
	}
	row := <-results
	if row.BusinessType != "LOGISTICS" || row.SourceBusinessID != 9901 || row.PaidAmount != "100.00" || row.RequestedAt == "" {
		t.Fatalf("wrong linked account: %+v", row)
	}
	rows, total, err := svc.ListSupplierRecon(ctx, tenant, SupplierReconFilter{Keyword: "LO-HISTORY", BusinessType: "LOGISTICS"}, op)
	if err != nil || len(rows) != 1 || total != 1 {
		t.Fatalf("not visible in finance: %d %d %v", len(rows), total, err)
	}
	if _, err = svc.RecordPOPayment(ctx, tenant, POPaymentInput{POID: row.POID, Amount: "50", PaidAt: "2026-11-15"}, op); err != nil {
		t.Fatal("linked history cannot record payment", err)
	}
	if _, err = svc.ClosePOPayment(ctx, tenant, row.POID, "OTHER", "History fixture closure", op); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CreateManualPayable(ctx, tenant, input, op); err == nil {
		t.Fatal("completed account duplicated")
	} else {
		wantAllocErr(t, err, "PR_LOGISTICS_ACCOUNT_EXISTS")
	}
	other, err := svc.CreateManualPayable(ctx, tenant+1, input, op)
	if err != nil {
		t.Fatal("tenant isolation", err)
	}
	defer func() {
		_, _ = svc.pool.Exec(ctx, `DELETE FROM payment_allocations WHERE tenant_id=$1`, tenant+1)
		_, _ = svc.pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenant+1)
	}()
	if other.POID == row.POID {
		t.Fatal("cross tenant account reused")
	}
	if _, err = svc.UpdateManualPayable(ctx, tenant, row.POID, "Other", "OTHER", "", op); err == nil {
		t.Fatal("source identity edited")
	}
}
func TestHistoricalLogisticsRejectsInvalidSource(t *testing.T) {
	ctx, svc, tenant, cleanup := reconTestPool(t)
	defer cleanup()
	for _, in := range []ManualPayableInput{{BusinessType: "LOGISTICS"}, {BusinessType: "MANUAL", SourceBusinessID: 1}, {BusinessType: "OTHER"}} {
		if _, err := svc.CreateManualPayable(ctx, tenant, in, Operator{ID: 77, Name: "Finance"}); err == nil {
			t.Fatal("invalid source accepted")
		}
	}
}

func TestManualPayableStillCreatesWithoutLogistics(t *testing.T) {
	ctx, svc, tenant, cleanup := reconTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	row, err := svc.CreateManualPayable(ctx, tenant, ManualPayableInput{SupplierName: "Manual supplier", OrderNo: "MANUAL-OPENING", Currency: "CNY", TotalAmount: "300", PaidAmount: "20", PaidAt: "2026-11-15"}, op)
	if err != nil {
		t.Fatal(err)
	}
	if row.SourceBusinessID != 0 || row.SupplierID != 0 || row.PaidAmount != "20.00" || row.RequestedAt != "" {
		t.Fatalf("ordinary manual payable changed: %+v", row)
	}
	if _, err = svc.RecordPOPayment(ctx, tenant, POPaymentInput{POID: row.POID, Amount: "30", PaidAt: "2026-11-15"}, op); err != nil {
		t.Fatal(err)
	}
}
