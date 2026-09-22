package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"os"
	"testing"
	"time"
)

type qualityManageScope struct{}

func (qualityManageScope) VisibleEmployees(_ context.Context, id int64, module string) (Visibility, error) {
	if module == "supplier" {
		return Visibility{All: id == 1}, nil
	}
	return Visibility{All: id == 1 || id == 2, EmployeeIDs: []int64{id}}, nil
}
func TestQualityManageGuardsAndAudit(t *testing.T) {
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
		_, _ = pool.Exec(ctx, `DELETE FROM quality_task_audit WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM quality_inspection_tasks WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_items WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1`, tenant)
	}()

	svc := New(pool, Deps{Scopes: qualityManageScope{}, Files: qualityFiles{}})
	admin := Operator{ID: 1, Name: "Admin"}
	inspector := Operator{ID: 2, Name: "Inspector"}
	q, err := svc.CreateQualityInspection(ctx, tenant, ApplyQualityInput{POID: po}, inspector)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Deletable {
		t.Fatal("new task not deletable")
	}
	if err = svc.DeleteQualityTask(ctx, tenant, q.ID, inspector); err == nil {
		t.Fatal("quality ALL must not grant delete")
	}
	if err = svc.DeleteQualityTask(ctx, tenant+1, q.ID, admin); err == nil {
		t.Fatal("cross tenant delete allowed")
	}
	if _, err = svc.UpdateQualityBasics(ctx, tenant+1, q.ID, ApplyQualityInput{}, admin); err == nil {
		t.Fatal("cross tenant edit allowed")
	}
	if _, err = svc.UpdateQualityBasics(ctx, tenant, q.ID, ApplyQualityInput{}, Operator{ID: 3}); err == nil {
		t.Fatal("out of scope edit allowed")
	}
	q, err = svc.UpdateQualityBasics(ctx, tenant, q.ID, ApplyQualityInput{ExpectedDate: "2026-10-01", Location: "Site", ContactName: "Contact", ContactPhone: "123", Remark: "Packing"}, inspector)
	if err != nil || q.ContactName != "Contact" || q.ExpectedDate != "2026-10-01" || q.POID != po || len(q.Lines) != 1 {
		t.Fatalf("edit: %+v %v", q, err)
	}
	key, _, _, err := svc.PresignQualityFile(ctx, tenant, q.ID, "quality.jpg")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.RegisterQualityFile(ctx, tenant, q.ID, RegisterQualityFileInput{Category: "PHOTO", Key: key, FileName: "quality.jpg"}, inspector)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.DeleteQualityTask(ctx, tenant, q.ID, admin); err == nil {
		t.Fatal("task with file deleted")
	}
	_, err = pool.Exec(ctx, `DELETE FROM quality_inspection_files WHERE tenant_id=$1 AND task_id=$2`, tenant, q.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.StartQualityTask(ctx, tenant, q.ID, inspector)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.DeleteQualityTask(ctx, tenant, q.ID, admin); err == nil {
		t.Fatal("started task deleted")
	}
	_, err = pool.Exec(ctx, `UPDATE quality_inspection_tasks SET status='COMPLETED' WHERE tenant_id=$1 AND id=$2`, tenant, q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.UpdateQualityBasics(ctx, tenant, q.ID, ApplyQualityInput{}, inspector); err == nil {
		t.Fatal("completed task edited")
	}
	_, err = pool.Exec(ctx, `UPDATE quality_inspection_tasks SET status='WAITING',started_at=NULL WHERE tenant_id=$1 AND id=$2`, tenant, q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.DeleteQualityTask(ctx, tenant, q.ID, admin); err != nil {
		t.Fatal(err)
	}
	var auditCount int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM quality_task_audit WHERE tenant_id=$1 AND task_id=$2 AND action IN ('DELETE','UPDATE_BASICS')`, tenant, q.ID).Scan(&auditCount)
	if err != nil || auditCount != 2 {
		t.Fatalf("audit missing: %d %v", auditCount, err)
	}
	recreated, err := svc.CreateQualityInspection(ctx, tenant, ApplyQualityInput{POID: po}, inspector)
	if err != nil || recreated.ID == q.ID || recreated.POID != po {
		t.Fatalf("recreate failed: %+v %v", recreated, err)
	}
}
