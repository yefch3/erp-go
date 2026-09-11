package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

type qualityFiles struct{}

func (qualityFiles) PresignPut(context.Context, string) (string, int32, error) {
	return "http://upload.local", 600, nil
}
func (qualityFiles) PresignGet(_ context.Context, key string) (string, error) {
	return "http://download.local/" + key, nil
}
func (qualityFiles) Put(context.Context, string, []byte, string) error { return nil }
func (qualityFiles) Remove(context.Context, string) error              { return nil }

func TestFactoryQualityLifecycleAndQuantityGuard(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	var req, po, item int64
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,ordered_qty,status,owner_id) VALUES($1,1,'CT-D5',1,$1,11,'P11','Coil',7,'MT',10,10,'ORDERED',55) RETURNING id`, tenant).Scan(&req); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_orders(tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,status,buyer_id,buyer_name,ordered_at) VALUES($1,'PO-D5',9,'F9','Factory 9','CNY',1000,'ORDERED',77,'Buyer',now()) RETURNING id`, tenant).Scan(&po); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_order_items(tenant_id,po_id,requirement_id,product_id,product_code,product_name,spec,uom_id,uom_code,qty,unit_price,amount) VALUES($1,$2,$3,11,'P11','Coil','Q235 / 2mm',7,'MT',10,100,1000) RETURNING id`, tenant, po, req).Scan(&item); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM quality_inspection_tasks WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_items WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1`, tenant)
	}()
	svc := New(pool, Deps{Files: qualityFiles{}})
	buyer := Operator{ID: 77, Name: "Buyer"}
	qc := Operator{ID: 88, Name: "QC"}
	task, err := svc.ApplyQualityInspection(ctx, tenant, ApplyQualityInput{POID: po, Location: "Factory 9", Lines: []ApplyQualityLine{{POItemID: item, Qty: "6"}}}, buyer)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "WAITING" || task.BatchNo != 1 || len(task.Lines) != 1 || task.Lines[0].Spec != "Q235 / 2mm" {
		t.Fatalf("task=%#v", task)
	}
	if _, err = svc.ApplyQualityInspection(ctx, tenant, ApplyQualityInput{POID: po, Lines: []ApplyQualityLine{{POItemID: item, Qty: "5"}}}, buyer); code(err) != "QUALITY_QTY_EXCEEDED" {
		t.Fatalf("expected quantity guard, got %v", err)
	}
	if task, err = svc.StartQualityTask(ctx, tenant, task.ID, qc); err != nil || task.Status != "IN_PROGRESS" {
		t.Fatalf("start=%#v err=%v", task, err)
	}
	task, err = svc.SubmitQualityRound(ctx, tenant, task.ID, SubmitQualityInput{Lines: []SubmitQualityLine{{TaskLineID: task.Lines[0].ID, Result: "PARTIAL", InspectedQty: "6", QualifiedQty: "4", UnqualifiedQty: "2", IssueDescription: "surface"}}}, qc)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "REINSPECTION" || task.Lines[0].QualifiedQty != "4.0000" || task.Lines[0].UnresolvedQty != "2.0000" {
		t.Fatalf("partial=%#v", task)
	}
	task, err = svc.DecideQualityRelease(ctx, tenant, task.ID, []DecideQualityLine{{TaskLineID: task.Lines[0].ID, Qty: "3"}}, buyer)
	if err != nil || task.Lines[0].ApprovedReleaseQty != "3.0000" {
		t.Fatalf("release=%#v err=%v", task, err)
	}
	key, _, _, err := svc.PresignQualityFile(ctx, tenant, task.ID, "现场 照片.jpg")
	if err != nil {
		t.Fatal(err)
	}
	task, err = svc.RegisterQualityFile(ctx, tenant, task.ID, RegisterQualityFileInput{Category: "PHOTO", Key: key, FileName: "现场 照片.jpg", ContentType: "image/jpeg", SizeBytes: 12}, qc)
	if err != nil || len(task.Files) != 1 || task.Files[0].DownloadURL == "" {
		t.Fatalf("files=%#v err=%v", task.Files, err)
	}
	task, err = svc.SubmitQualityRound(ctx, tenant, task.ID, SubmitQualityInput{Lines: []SubmitQualityLine{{TaskLineID: task.Lines[0].ID, Result: "PASS", InspectedQty: "2", QualifiedQty: "2", UnqualifiedQty: "0"}}}, qc)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "COMPLETED" || len(task.Rounds) != 2 || task.Lines[0].ApprovedReleaseQty != "6.0000" {
		t.Fatalf("completed=%#v", task)
	}
	if _, err = svc.RegisterQualityFile(ctx, tenant, task.ID, RegisterQualityFileInput{Category: "REPORT", Key: key + "-2", FileName: "late.pdf"}, qc); code(err) != "QUALITY_COMPLETED_IMMUTABLE" {
		t.Fatalf("expected immutable, got %v", err)
	}
	if _, err = svc.RegisterQualityFile(ctx, tenant, task.ID, RegisterQualityFileInput{Category: "REPORT", Key: key + "-3", FileName: "supplement.pdf", Supplemental: true}, qc); err != nil {
		t.Fatal(err)
	}
}
