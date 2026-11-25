package app

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

type qualityFiles struct{}

type recordingQualityFiles struct {
	qualityFiles
	removed []string
	err     error
}

func (s *recordingQualityFiles) Remove(_ context.Context, key string) error {
	if s.err != nil {
		return s.err
	}
	s.removed = append(s.removed, key)
	return nil
}

func TestQualityAttachmentsAppendAndDeleteIndividually(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	ctx := context.Background()
	task := f.apply(t)
	files := &recordingQualityFiles{}
	svc := New(f.pool, Deps{Files: files})
	op := Operator{ID: 88, Name: "QC"}
	for _, category := range []string{"PHOTO", "REPORT", "OTHER"} {
		for _, name := range []string{"first.txt", "second.txt"} {
			key, _, _, err := svc.PresignQualityFile(ctx, f.tenant, task.ID, name)
			if err != nil {
				t.Fatal(err)
			}
			input := RegisterQualityFileInput{Category: category, Key: key, FileName: name}
			for attempt := 0; attempt < 2; attempt++ {
				if _, err := svc.RegisterQualityFile(ctx, f.tenant, task.ID, input, op); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	registered, err := svc.GetQualityTask(ctx, f.tenant, task.ID)
	if err != nil || len(registered.Files) != 6 {
		t.Fatalf("expected 6 independent files after retries: %+v (%v)", registered.Files, err)
	}
	counts := map[string]int{}
	for _, file := range registered.Files {
		counts[file.Category]++
		if file.DownloadURL == "" {
			t.Fatalf("missing download address for %d", file.ID)
		}
	}
	if counts["PHOTO"] != 2 || counts["REPORT"] != 2 || counts["OTHER"] != 2 {
		t.Fatalf("categories lost: %v", counts)
	}
	first := registered.Files[0]
	for _, pair := range [][2]int64{{f.tenant + 1, task.ID}, {f.tenant, task.ID + 1}} {
		if err := svc.DeleteQualityFile(ctx, pair[0], pair[1], first.ID); code(err) != "QUALITY_FILE_NOT_FOUND" {
			t.Fatalf("delete should be scoped to tenant and task: %v", err)
		}
	}
	files.err = errors.New("storage unavailable")
	if err := svc.DeleteQualityFile(ctx, f.tenant, task.ID, first.ID); !errors.Is(err, files.err) {
		t.Fatalf("expected the storage failure: %v", err)
	}
	files.err = nil
	if err := svc.DeleteQualityFile(ctx, f.tenant, task.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	remaining, err := svc.GetQualityTask(ctx, f.tenant, task.ID)
	if err != nil || len(remaining.Files) != 5 || len(files.removed) != 1 || files.removed[0] != first.ObjectKey {
		t.Fatalf("expected only one file removed: %+v / %v (%v)", remaining.Files, files.removed, err)
	}
	for _, file := range remaining.Files {
		if file.ID == first.ID {
			t.Fatal("removed file is still listed")
		}
	}
}

func TestQualityRoundLeavesUnsubmittedSpecsPending(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	ctx := context.Background()
	var requirement int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO purchase_requirements
		(tenant_id, contract_id, contract_no, contract_version_id, contract_item_id, product_id, product_name, uom_code, required_qty, ordered_qty, status)
		VALUES ($1, 991, 'CT-QUALITY', 1, 993, 12, 'Pipe', 'PCS', 4, 4, 'ORDERED') RETURNING id`, f.tenant).Scan(&requirement); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO purchase_order_items
		(tenant_id, po_id, requirement_id, product_id, product_name, uom_code, qty, unit_price, amount)
		VALUES ($1, $2, $3, 12, 'Pipe', 'PCS', 4, 100, 400)`, f.tenant, f.po, requirement); err != nil {
		t.Fatal(err)
	}
	task := f.apply(t)
	if len(task.Lines) != 2 {
		t.Fatalf("expected two specifications: %+v", task.Lines)
	}
	firstID, secondID := task.Lines[0].ID, task.Lines[1].ID
	qc := Operator{ID: 88, Name: "QC"}
	partial, err := f.svc.SubmitQualityRound(ctx, f.tenant, task.ID, SubmitQualityInput{
		Lines: []SubmitQualityLine{{TaskLineID: firstID, Result: "PASS", InspectedQty: "10", QualifiedQty: "10", UnqualifiedQty: "0"}},
	}, qc)
	if err != nil {
		t.Fatal(err)
	}
	if partial.Status == "COMPLETED" || len(partial.Rounds) != 1 || len(partial.Rounds[0].Lines) != 1 ||
		partial.Lines[0].FinalResult != "PASS" || partial.Lines[1].FinalResult != "PENDING" ||
		partial.Lines[1].QualifiedQty != "0.0000" || partial.Lines[1].UnresolvedQty != "4.0000" {
		t.Fatalf("submitting one specification changed an unsubmitted line: %+v", partial)
	}
	completed, err := f.svc.SubmitQualityRound(ctx, f.tenant, task.ID, SubmitQualityInput{
		Lines: []SubmitQualityLine{{TaskLineID: secondID, Result: "PASS", InspectedQty: "4", QualifiedQty: "4", UnqualifiedQty: "0"}},
	}, qc)
	if err != nil || completed.Status != "COMPLETED" || len(completed.Rounds) != 2 {
		t.Fatalf("expected the same task to complete with both rounds: %+v, %v", completed, err)
	}
}

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
	const purchaseRemark = "工厂 A：检查锌层与包装\n按合同 CT-D5 分开标识；<b>原文</b>"
	const inspectionRemark = "申请质检：请联系现场负责人"
	if _, err = pool.Exec(ctx, `UPDATE purchase_orders SET remark=$3 WHERE tenant_id=$1 AND id=$2`, tenant, po, purchaseRemark); err != nil {
		t.Fatal(err)
	}
	task, err := svc.ApplyQualityInspection(ctx, tenant, ApplyQualityInput{POID: po, Location: "Factory 9", Remark: inspectionRemark}, buyer)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "WAITING" || task.BatchNo != 1 || len(task.Lines) != 1 || task.Lines[0].Spec != "Q235 / 2mm" || task.Lines[0].RequestedQty != "10.0000" {
		t.Fatalf("task=%#v", task)
	}
	if task.PurchaseOrderRemark != purchaseRemark || task.Remark != inspectionRemark {
		t.Fatalf("purchase and inspection remarks must be separate: %+v", task)
	}
	// Existing tasks read the current order remark; no reapplication/backfill.
	const updatedRemark = "工厂 B：更新包装检查要求\n不可混装"
	if _, err = pool.Exec(ctx, `UPDATE purchase_orders SET remark=$3 WHERE tenant_id=$1 AND id=$2`, tenant, po, updatedRemark); err != nil {
		t.Fatal(err)
	}
	task, err = svc.GetQualityTask(ctx, tenant, task.ID)
	if err != nil || task.PurchaseOrderRemark != updatedRemark || task.Remark != inspectionRemark {
		t.Fatalf("existing task failed to show updated PO remark: %+v %v", task, err)
	}
	if _, err = svc.GetQualityTask(ctx, tenant+1, task.ID); code(err) != "QUALITY_TASK_NOT_FOUND" {
		t.Fatalf("other tenant can read remarks: %v", err)
	}
	scoped := New(pool, Deps{Scopes: &listAccess{}})
	if err = scoped.AuthorizeQualityTask(ctx, tenant, task.ID, Operator{ID: 999}); code(err) != "QUALITY_TASK_NOT_FOUND" {
		t.Fatalf("out-of-scope reader can reach order remark: %v", err)
	}
	if err = scoped.AuthorizeQualityTask(ctx, tenant, task.ID, buyer); err != nil {
		t.Fatalf("authorized buyer lost task access: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE purchase_orders SET remark='' WHERE tenant_id=$1 AND id=$2`, tenant, po); err != nil {
		t.Fatal(err)
	}
	empty, err := svc.GetQualityTask(ctx, tenant, task.ID)
	if err != nil || empty.PurchaseOrderRemark != "" || empty.Remark != inspectionRemark {
		t.Fatalf("empty PO remark must not use the inspection remark as fallback: %+v %v", empty, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE purchase_orders SET remark=$3 WHERE tenant_id=$1 AND id=$2`, tenant, po, updatedRemark); err != nil {
		t.Fatal(err)
	}
	duplicate, err := svc.ApplyQualityInspection(ctx, tenant, ApplyQualityInput{POID: po}, buyer)
	if err != nil || duplicate.ID != task.ID {
		t.Fatalf("repeat application should return the one existing task: task=%#v err=%v", duplicate, err)
	}
	// The purchase request is the handoff. Quality records the first round
	// directly without a redundant accept/start transition.
	task, err = svc.SubmitQualityRound(ctx, tenant, task.ID, SubmitQualityInput{Lines: []SubmitQualityLine{{TaskLineID: task.Lines[0].ID, Result: "PARTIAL", InspectedQty: "10", QualifiedQty: "8", UnqualifiedQty: "2", IssueDescription: "surface"}}}, qc)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "REINSPECTION" || task.ProcurementHandlingStatus != "PENDING" || task.Lines[0].QualifiedQty != "8.0000" || task.Lines[0].UnresolvedQty != "2.0000" {
		t.Fatalf("partial=%#v", task)
	}
	if _, err = svc.DecideQualityRelease(ctx, tenant, task.ID, []DecideQualityLine{{TaskLineID: task.Lines[0].ID, Qty: "3"}}, buyer); code(err) != "QUALITY_PARTIAL_SHIPMENT_DISABLED" {
		t.Fatalf("partial release must be disabled, got %v", err)
	}
	todos, err := svc.ListQualityProcurementTodos(ctx, tenant, buyer)
	if err != nil || len(todos) != 1 {
		t.Fatalf("buyer todo=%#v err=%v", todos, err)
	}
	task, err = svc.RecordQualityProcurementHandling(ctx, tenant, po, task.ID, "REWORK", "工厂返工后复检", buyer)
	if err != nil || task.ProcurementHandlingStatus != "COMPLETED" {
		t.Fatalf("handling=%#v err=%v", task, err)
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
	if task.Status != "COMPLETED" || len(task.Rounds) != 2 || task.Lines[0].ApprovedReleaseQty != "10.0000" {
		t.Fatalf("completed=%#v", task)
	}
	if task.PurchaseOrderRemark != updatedRemark || task.Remark != inspectionRemark {
		t.Fatalf("completing inspection changed source remarks: %+v", task)
	}
	if _, err = svc.RegisterQualityFile(ctx, tenant, task.ID, RegisterQualityFileInput{Category: "REPORT", Key: key + "-2", FileName: "late.pdf"}, qc); code(err) != "QUALITY_COMPLETED_IMMUTABLE" {
		t.Fatalf("expected immutable, got %v", err)
	}
	if _, err = svc.RegisterQualityFile(ctx, tenant, task.ID, RegisterQualityFileInput{Category: "REPORT", Key: key + "-3", FileName: "supplement.pdf", Supplemental: true}, qc); err != nil {
		t.Fatal(err)
	}
}
