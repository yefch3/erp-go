package app

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOrderFollowUpsPreserveBusinessFactsAndHistory(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	ctx := context.Background()
	t.Cleanup(func() { _, _ = f.pool.Exec(ctx, `DELETE FROM purchase_order_follow_ups WHERE tenant_id=$1`, f.tenant) })
	op := Operator{ID: 77, Name: "Buyer"}
	if _, err := f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SCHEDULED", PlannedDate: time.Now().Format("2006-01-02"), Remark: "preserve historical milestone"}, op); err != nil {
		t.Fatal(err)
	}
	before, err := f.svc.GetOrderExecution(ctx, f.tenant, f.po)
	if err != nil {
		t.Fatal(err)
	}
	orderBefore, err := f.svc.GetOrder(ctx, f.tenant, f.po)
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.svc.AddOrderFollowUp(ctx, f.tenant, f.po, "  Supplier preparing goods  ", op)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.svc.AddOrderFollowUp(ctx, f.tenant, f.po, "Waiting for inspection", op)
	if err != nil {
		t.Fatal(err)
	}
	after, err := f.svc.GetOrderExecution(ctx, f.tenant, f.po)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.FollowUps) != 2 || after.FollowUps[0].ID != second.ID || after.FollowUps[1].ID != first.ID || first.Note != "Supplier preparing goods" || first.CreatedBy != "Buyer" || first.CreatedAt == "" {
		t.Fatalf("missing append-only audit: %+v", after.FollowUps)
	}
	if !reflect.DeepEqual(before.Milestones, after.Milestones) || !reflect.DeepEqual(before.Confirmations, after.Confirmations) || !reflect.DeepEqual(before.Inspections, after.Inspections) {
		t.Fatal("notes changed business facts or historical records")
	}
	orderAfter, err := f.svc.GetOrder(ctx, f.tenant, f.po)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(orderBefore, orderAfter) {
		t.Fatal("notes mutated the purchase order")
	}
	for _, note := range []string{"   ", strings.Repeat("字", 4001)} {
		if _, err := f.svc.AddOrderFollowUp(ctx, f.tenant, f.po, note, op); err == nil {
			t.Fatal("invalid note accepted")
		}
	}
	if _, err := f.svc.AddOrderFollowUp(ctx, f.tenant+1, f.po, "wrong tenant", op); err == nil {
		t.Fatal("cross-tenant note accepted")
	}
	if _, err := f.pool.Exec(ctx, `UPDATE purchase_orders SET closed_at=now() WHERE tenant_id=$1 AND id=$2`, f.tenant, f.po); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddOrderFollowUp(ctx, f.tenant, f.po, "closed order", op); err == nil {
		t.Fatal("closed order is not read only")
	}
}
