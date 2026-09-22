package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/contractflow"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestHistoricalRecordWithoutItems(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer func() {
		for _, tbl := range []string{"contract_workflows", "contract_attachments", "contract_items", "contract_versions", "contracts", "outbox_events"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+tbl+" WHERE tenant_id=$1", tenant)
		}
	}()
	svc := New(pool, Deps{Customers: dueCustomerStub{}, Products: dueProductStub{}, Rates: d2OfferRates{}, Numbering: &dueNumberingStub{}, Directory: dueDirectoryStub{}, Files: d2Files{}})
	for _, state := range []string{"EXECUTING", "COMPLETED"} {
		in := ExistingContractInput{CustomerID: 7, Currency: "USD", ExternalContractNo: "ARCHIVE-" + state + strconv.FormatInt(tenant, 10), SignedDate: "2025-01-02", SignedFileKey: "contract-imports/" + strconv.FormatInt(tenant, 10) + "/" + state + "-signed.pdf", SignedFileName: "signed.pdf", History: contractflow.History{RecordOnly: true, TotalAmount: "1200.50", Status: state}}
		view, err := svc.ImportExistingContract(ctx, tenant, in, Operator{ID: 5, Name: "Sales"})
		if err != nil {
			t.Fatal(err)
		}
		if view.Contract.EntrySource != "HISTORICAL_RECORD" || view.Contract.Status != state || len(view.Items) != 0 || view.Version.TotalAmount != "1200.50" {
			t.Fatalf("bad archive: %+v", view)
		}
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM outbox_events WHERE tenant_id=$1", tenant).Scan(&n); err != nil || n != 0 {
			t.Fatalf("archive emitted events %d: %v", n, err)
		}
		if _, err := svc.q.ContractReceiptProgress(ctx, store.ContractReceiptProgressParams{TenantID: tenant, ContractID: view.Contract.ID}); err == nil {
			t.Fatal("archive incorrectly treated as receivable")
		}
		if _, err := svc.GetContract(ctx, tenant+1, view.Contract.ID, 0); err == nil {
			t.Fatal("cross-tenant archive read")
		}
		if _, err := svc.ConfirmContractExecutionCondition(ctx, tenant, view.Contract.ID, ConditionNoPrepaymentRequired, "test", Operator{ID: 5}); err == nil {
			t.Fatal("archive released downstream")
		}
	}
}
func TestHistoricalRecordValidation(t *testing.T) {
	good := ExistingContractInput{ExternalContractNo: "old-123", SignedDate: "2025-01-02", History: contractflow.History{RecordOnly: true, TotalAmount: "100.00", Status: "EXECUTING"}}
	for _, amount := range []string{"", "-1", "0", "NaN", "1.001", "10000000000000000"} {
		in := good
		in.History.TotalAmount = amount
		if validateHistoricalOrders(&in) == nil {
			t.Fatalf("accepted amount %q", amount)
		}
	}
	in := good
	in.History.Status = "APPROVED"
	if validateHistoricalOrders(&in) == nil {
		t.Fatal("accepted arbitrary status")
	}
	in = good
	in.Items = []ItemInput{{ProductName: "unexpected"}}
	if validateHistoricalOrders(&in) == nil {
		t.Fatal("accepted hidden items")
	}
	in = good
	if err := validateHistoricalOrders(&in); err != nil {
		t.Fatal(err)
	}
}
