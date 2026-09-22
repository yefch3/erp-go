package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"testing"
	"time"
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
