package app

import (
	"context"
	"testing"
)

// Both paths use the real migrated database constraint. Quality owns its
// conclusion; a pending buyer follow-up must not prevent a passing retest.
func TestQualityRetestWithAndWithoutBuyerHandling(t *testing.T) {
	for _, manual := range []bool{false, true} {
		name := "without_buyer_handling"
		if manual {
			name = "with_buyer_handling"
		}
		t.Run(name, func(t *testing.T) {
			f := newDeliveryQualityFixture(t)
			ctx := context.Background()
			buyer := Operator{ID: 77, Name: "Buyer"}
			qc := Operator{ID: 88, Name: "QC"}
			task := f.apply(t)
			task, err := f.svc.SubmitQualityRound(ctx, f.tenant, task.ID, SubmitQualityInput{Lines: []SubmitQualityLine{{TaskLineID: task.Lines[0].ID, Result: "PARTIAL", InspectedQty: "10", QualifiedQty: "8", UnqualifiedQty: "2", IssueDescription: "surface defect"}}}, qc)
			if err != nil {
				t.Fatal(err)
			}
			if task.Status != "REINSPECTION" || task.ProcurementHandlingStatus != "PENDING" || task.ProcurementHandlingAction != "" {
				t.Fatalf("first round=%+v", task)
			}
			firstRoundID := task.Rounds[0].ID
			f.check(t, DeliveryQualityInput{POIDs: []int64{f.po}}, false)
			todos, err := f.svc.ListQualityProcurementTodos(ctx, f.tenant, buyer)
			if err != nil || len(todos) != 1 {
				t.Fatalf("pending todos=%v err=%v", todos, err)
			}
			if manual {
				task, err = f.svc.RecordQualityProcurementHandling(ctx, f.tenant, f.po, task.ID, "REWORK", "buyer confirmed rework", buyer)
				if err != nil {
					t.Fatal(err)
				}
				f.check(t, DeliveryQualityInput{POIDs: []int64{f.po}}, false)
			}
			// Partial success on a retest still leaves the unresolved balance and
			// buyer follow-up pending. It must not close/release an order early.
			task, err = f.svc.SubmitQualityRound(ctx, f.tenant, task.ID, SubmitQualityInput{Lines: []SubmitQualityLine{{TaskLineID: task.Lines[0].ID, Result: "PARTIAL", InspectedQty: "2", QualifiedQty: "1", UnqualifiedQty: "1"}}}, qc)
			if err != nil {
				t.Fatal(err)
			}
			if task.Status != "REINSPECTION" || task.ProcurementHandlingStatus != "PENDING" || task.Lines[0].UnresolvedQty != "1.0000" {
				t.Fatalf("partial retest=%+v", task)
			}
			f.check(t, DeliveryQualityInput{POIDs: []int64{f.po}}, false)
			if manual {
				task, err = f.svc.RecordQualityProcurementHandling(ctx, f.tenant, f.po, task.ID, "REWORK", "buyer confirmed final rework", buyer)
				if err != nil {
					t.Fatal(err)
				}
			}
			task, err = f.svc.SubmitQualityRound(ctx, f.tenant, task.ID, SubmitQualityInput{Lines: []SubmitQualityLine{{TaskLineID: task.Lines[0].ID, Result: "PASS", InspectedQty: "1", QualifiedQty: "1", UnqualifiedQty: "0"}}}, qc)
			if err != nil {
				t.Fatalf("passing retest must commit even without buyer handling: %v", err)
			}
			if task.Status != "COMPLETED" || task.ProcurementHandlingStatus != "COMPLETED" || len(task.Rounds) != 3 || task.Rounds[0].ID != firstRoundID || task.Lines[0].QualifiedQty != "10.0000" || task.Lines[0].UnresolvedQty != "0.0000" || task.Lines[0].ApprovedReleaseQty != "10.0000" {
				t.Fatalf("completed=%+v", task)
			}
			if manual {
				if task.ProcurementHandlingAction != "REWORK" || task.ProcurementHandlingNote != "buyer confirmed final rework" || task.ProcurementHandledByName != "Buyer" {
					t.Fatalf("manual handling changed: %+v", task)
				}
			} else if task.ProcurementHandlingAction != "" || task.ProcurementHandlingNote == "" || task.ProcurementHandledByName != "" {
				t.Fatalf("automatic closure must not invent manual buyer handling: %+v", task)
			}
			todos, err = f.svc.ListQualityProcurementTodos(ctx, f.tenant, buyer)
			if err != nil || len(todos) != 0 {
				t.Fatalf("stale buyer todos=%v err=%v", todos, err)
			}
			f.check(t, DeliveryQualityInput{POIDs: []int64{f.po}}, true)
			var receiptCount int
			if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM purchase_receipts WHERE tenant_id=$1 AND po_id=$2`, f.tenant, f.po).Scan(&receiptCount); err != nil || receiptCount != 0 {
				t.Fatalf("quality must not deliver goods: count=%d err=%v", receiptCount, err)
			}
			if _, err = f.svc.SubmitQualityRound(ctx, f.tenant, task.ID, SubmitQualityInput{Lines: []SubmitQualityLine{{TaskLineID: task.Lines[0].ID, Result: "PASS", InspectedQty: "1", QualifiedQty: "1", UnqualifiedQty: "0"}}}, qc); code(err) != "QUALITY_ROUND_INVALID" {
				t.Fatalf("duplicate final round=%v", err)
			}
			saved, err := f.svc.GetQualityTask(ctx, f.tenant, task.ID)
			if err != nil || len(saved.Rounds) != 3 {
				t.Fatalf("duplicate changed history: %+v err=%v", saved, err)
			}
		})
	}
}
