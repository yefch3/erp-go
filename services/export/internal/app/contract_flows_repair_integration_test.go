package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
	"os"
	"testing"
	"time"
)

func TestCategoryOfferContractLifecycle(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN required")
	}
	pool, err := pgdb.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	ctx := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: tenant, EmployeeID: 3, Name: "Manager"})
	body, source := categoryWorkspaceFixture()
	source.ID = "991"
	source.OwnerID = "1"
	source.Owner = "Sales"
	source.State = "INQUIRING"
	source.Number = "CATEGORY-REPAIR"
	source.Body.CustomerID = "7"
	source.Body.Customer = "Customer"
	body.Delivery = "2026-12-01"
	svc := New(pool, Deps{OfferSource: &d2OfferSource{inquiry: source}, Scopes: d2OfferScopes{}, Rates: d2OfferRates{}, Numbering: &d2OfferNumber{}, Customers: dueCustomerStub{}, Approvals: d2Approvals{}, Files: d2Files{}})
	call := func(cmd OfferCommand) (OfferView, error) {
		raw, _ := json.Marshal(cmd)
		out, e := svc.CustomerOffer(ctx, string(raw))
		var v OfferView
		if e == nil {
			e = json.Unmarshal([]byte(out), &v)
		}
		return v, e
	}
	saved, err := call(OfferCommand{Action: "stage_selections", CaseID: "991", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = call(OfferCommand{Action: "confirm", CaseID: "991", Revision: saved.Revision}); err == nil {
		t.Fatal("unselected alternatives confirmed")
	}
	saved.Body.Negotiations[0].Status = "AGREED"
	saved.Body.Negotiations[0].ProposedPrice = "15.25"
	saved, err = call(OfferCommand{Action: "save", CaseID: "991", Revision: saved.Revision, Body: saved.Body})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := call(OfferCommand{Action: "confirm", CaseID: "991", Revision: saved.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if len(confirmed.Body.Lines) != 1 || confirmed.Body.Total != "305.00" {
		t.Fatal("alternatives duplicated", confirmed.Body)
	}
	again, err := call(OfferCommand{Action: "confirm", CaseID: "991", Revision: saved.Revision})
	if err != nil || again.ContractID != confirmed.ContractID {
		t.Fatal("confirmation duplicated", err)
	}
	id := offerID(confirmed.ContractID)
	v, err := svc.GetContract(ctx, tenant, id, 0)
	if err != nil || v.Contract.SalesEmployeeID != 1 || v.Version.TotalAmount != "305.00" {
		t.Fatal("owner or amount mismatch", v, err)
	}
	owner := Operator{ID: 1, Name: "Sales"}
	sales := grpcx.WithOperator(ctx, grpcx.Operator{TenantID: tenant, EmployeeID: 1, Name: "Sales"})
	if _, _, err = svc.SubmitContract(sales, tenant, id, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ApplyApprovalDecision(sales, tenant, id, "APPROVED", func(context.Context, pgx.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	_, err = svc.RegisterContractFile(sales, tenant, id, FileInput{Key: fmt.Sprintf("contracts/%d/%d/signed.pdf", tenant, id), FileName: "signed.pdf", Kind: "SIGNED"}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SignContract(sales, tenant, id, "", "", "", owner); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ConfirmContractExecutionCondition(sales, tenant, id, ConditionNoPrepaymentRequired, "test", owner); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE tenant_id=$1 AND event_type='ContractEffective'`, tenant).Scan(&count); err != nil || count != 1 {
		t.Fatal("no release event", count, err)
	}
}

func TestDirectContractRepairLifecycle(t *testing.T) {
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
	ctx = grpcx.WithOperator(ctx, grpcx.Operator{TenantID: tenant, EmployeeID: 1, Name: "Sales"})
	svc := New(pool, Deps{Customers: dueCustomerStub{}, Products: dueProductStub{}, Rates: d2OfferRates{}, Numbering: &d2OfferNumber{}, Approvals: d2Approvals{}, Files: d2Files{}, Scopes: d2OfferScopes{}})
	op := Operator{ID: 1, Name: "Sales"}
	v, err := svc.CreateContract(ctx, tenant, DirectContractInput{CustomerID: 7, Currency: "USD", Terms: Terms{DeliveryDate: "2026-12-01"}, Items: []ItemInput{{ProductName: "HRC", Spec: "3mm", UomCode: "MT", Qty: "100", UnitPrice: "12.3456", Remark: "dry packing"}, {ProductName: "HRC", Spec: "4mm", UomCode: "MT", Qty: "100", UnitPrice: "20"}}}, op)
	if err != nil {
		t.Fatal(err)
	}
	id := v.Contract.ID
	file := func(name string) {
		t.Helper()
		_, e := svc.RegisterContractFile(ctx, tenant, id, FileInput{Key: fmt.Sprintf("contracts/%d/%d/%s", tenant, id, name), FileName: name, Kind: "SIGNED"}, op)
		if e != nil {
			t.Fatal(e)
		}
	}
	file("before-edit.pdf")
	v, err = svc.UpdateContract(ctx, tenant, id, Terms{DeliveryDate: "2026-12-02"}, nil, ContractEditMeta{}, op)
	if err != nil {
		t.Fatal(err)
	}
	if v.Items[0].Remark != "dry packing" || v.Items[0].Spec != "3mm" {
		t.Fatal("terms-only edit lost item metadata")
	}
	n, err := svc.q.CountSignedAttachments(ctx, store.CountSignedAttachmentsParams{TenantID: tenant, ContractVersionID: &v.Version.ID})
	if err != nil || n != 0 {
		t.Fatal("stale signature still valid", n, err)
	}
	files, err := svc.ListContractFiles(ctx, tenant, id, op)
	if err != nil || len(files) != 1 || files[0].Row.Kind != "STALE_SIGNED" {
		t.Fatal("old evidence not retained", files, err)
	}
	if _, _, err = svc.SubmitContract(ctx, tenant, id, op); err != nil {
		t.Fatal(err)
	}
	claim := func(context.Context, pgx.Tx) error { return nil }
	if _, err = svc.ApplyApprovalDecision(ctx, tenant, id, "APPROVED", claim); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SignContract(ctx, tenant, id, "", "", "", op); err == nil {
		t.Fatal("stale evidence started contract")
	}
	file("approved.pdf")
	if _, err = svc.SignContract(ctx, tenant, id, "", "", "", op); err != nil {
		t.Fatal(err)
	}
	allowed, _, err := svc.CheckContractExecution(ctx, tenant, id)
	if err != nil || allowed {
		t.Fatal("execution before finance", err)
	}
	for i := 0; i < 2; i++ {
		if _, err = svc.ConfirmContractExecutionCondition(ctx, tenant, id, ConditionNoPrepaymentRequired, "test", op); err != nil {
			t.Fatal(err)
		}
	}
	var events int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE tenant_id=$1 AND event_type='ContractEffective'`, tenant).Scan(&events); err != nil || events != 1 {
		t.Fatal("duplicate release", events, err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO contract_shipments(tenant_id,contract_id,contract_item_id,product_id,outbound_no,qty,shipped_at) VALUES($1,$2,$3,0,'REPAIR-TEST',20,now())`, tenant, id, v.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	progress, err := svc.ShipmentProgress(ctx, tenant, id, v.Version.ID)
	if err != nil || len(progress) != 2 {
		t.Fatal("specs merged", progress, err)
	}
	if progress[0].ShippedQty != "20.0000" || progress[1].ShippedQty != "0.0000" {
		t.Fatal("shipment crossed specs", progress)
	}
	changed, err := svc.ChangeContract(ctx, tenant, id, Terms{DeliveryDate: "2026-12-03"}, "delivery only", nil, op)
	if err != nil {
		t.Fatal(err)
	}
	progress, err = svc.ShipmentProgress(ctx, tenant, id, changed.Version.ID)
	if err != nil || len(progress) != 2 || progress[0].ShippedQty != "20.0000" {
		t.Fatal("lost cross-version shipment", progress, err)
	}
}
