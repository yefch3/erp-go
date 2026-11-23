package app

import (
	"context"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func TestContractWithdrawalIdentityAndCompletedDecision(t *testing.T) {
	svc, cleanup := newSeedTestService(t, stubDirectory{managers: map[int32][]int64{1: {900}}})
	tenant := time.Now().UnixNano()
	defer cleanup(tenant)
	actor := func(ten, id int64) context.Context {
		return grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: ten, EmployeeID: id})
	}
	input := SubmitInput{BizType: "CONTRACT", BizID: 1, BizNo: "WITHDRAW", SubmitterID: 500, SubmitterName: "sales", Amount: "100"}
	inst, tasks, err := svc.Submit(context.Background(), tenant, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Withdraw(actor(tenant+1, 500), tenant+1, inst.ID, 500, "test"); err == nil {
		t.Fatal("cross tenant withdrawal")
	}
	if err := svc.Withdraw(actor(tenant, 900), tenant, inst.ID, 900, "test"); err == nil {
		t.Fatal("approver withdrew another person's application")
	}
	if err := svc.Withdraw(actor(tenant, 500), tenant, inst.ID, 900, "test"); err == nil {
		t.Fatal("forged actor")
	}
	if err := svc.Withdraw(actor(tenant, 500), tenant, inst.ID, 500, ""); apierr.CodeFromError(err) != "AP_WITHDRAW_REASON" {
		t.Fatalf("expected contract withdrawal reason to be required: %v", err)
	}
	if err := svc.Withdraw(actor(tenant, 500), tenant, inst.ID, 500, "revise contract"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Withdraw(actor(tenant, 500), tenant, inst.ID, 500, "retry"); err != nil {
		t.Fatal("withdraw retry", err)
	}
	if _, _, err := svc.Act(context.Background(), tenant, 900, tasks[0].ID, ActionApprove, ""); err == nil {
		t.Fatal("approved a cancelled task")
	}
	input.BizID = 2
	inst, tasks, err = svc.Submit(context.Background(), tenant, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Act(context.Background(), tenant, 900, tasks[0].ID, ActionApprove, ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.Withdraw(actor(tenant, 500), tenant, inst.ID, 500, "too late"); err == nil {
		t.Fatal("withdrew completed approval")
	}
}

func TestPurchaseOrderApprovalWithdrawal(t *testing.T) {
	ctx := t.Context()
	svc, cleanup := newSeedTestService(t, stubDirectory{
		managers: map[int32][]int64{1: {900}},
		byCode: map[string][]int64{
			"PROCUREMENT_MANAGER": {600},
			"BOSS":                {601},
			"SUPER_ADMIN":         {602},
		},
	})
	tenant := time.Now().UnixNano()
	defer cleanup(tenant)
	for index, user := range []int64{500, 600, 601, 602} {
		bizID := int64(index + 1)
		input := SubmitInput{BizType: "PURCHASE_ORDER", BizID: bizID, BizNo: "PO-WITHDRAW", SubmitterID: 500, SubmitterName: "buyer", Amount: "100"}
		inst, tasks, err := svc.Submit(ctx, tenant, input)
		if err != nil || len(tasks) == 0 {
			t.Fatalf("submission: %+v, %v", tasks, err)
		}
		if eligible, err := svc.PurchaseOrderWithdrawalInstance(ctx, tenant, bizID, user); err != nil || eligible != inst.ID {
			t.Fatalf("eligible actor %d: %d, %v", user, eligible, err)
		}
		other := grpcx.WithOperator(ctx, grpcx.Operator{TenantID: tenant, EmployeeID: 900})
		if eligible, err := svc.PurchaseOrderWithdrawalInstance(other, tenant, bizID, 900); err != nil || eligible != 0 {
			t.Fatalf("unprivileged approver: %d, %v", eligible, err)
		}
		if err := svc.Withdraw(other, tenant, inst.ID, 900, "incorrect amount"); apierr.CodeFromError(err) != "AP_WITHDRAW_OWNER" {
			t.Fatalf("unprivileged withdrawal: %v", err)
		}
		actor := grpcx.WithOperator(ctx, grpcx.Operator{TenantID: tenant, EmployeeID: user})
		if err := svc.Withdraw(actor, tenant, inst.ID, user, ""); err != nil {
			t.Fatal(err)
		}
		var withdrawnBy int64
		var reason string
		err = svc.pool.QueryRow(ctx, `SELECT (biz_summary->>'withdrawn_by')::bigint, coalesce(biz_summary->>'withdrawal_reason','')
			FROM approval_instances WHERE tenant_id=$1 AND id=$2 AND status='CANCELLED'`, tenant, inst.ID).Scan(&withdrawnBy, &reason)
		if err != nil || withdrawnBy != user || reason != "" {
			t.Fatalf("withdrawal audit: %d, %q, %v", withdrawnBy, reason, err)
		}
		if _, _, err := svc.Act(ctx, tenant, tasks[0].AssigneeID, tasks[0].ID, ActionApprove, ""); err == nil {
			t.Fatal("approved a withdrawn task")
		}
		next, _, err := svc.Submit(ctx, tenant, input)
		if err != nil || next.ID == inst.ID {
			t.Fatalf("resubmission: %d, %v", next.ID, err)
		}
	}
}
