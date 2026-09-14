package app

import (
	"context"
	"testing"
	"time"
)

func TestD2ContractOneSuperiorAndRetry(t *testing.T) {
	svc, cleanup := newSeedTestService(t, stubDirectory{managers: map[int32][]int64{1: {900}, 2: {901}}})
	tenant := time.Now().UnixNano()
	defer cleanup(tenant)
	ctx := context.Background()
	input := SubmitInput{BizType: "CONTRACT", BizID: 1, BizNo: "D2-RETRY", BizSummary: `{"approval_request_key":"attempt-one"}`, SubmitterID: 500, SubmitterName: "sales", Amount: "100"}
	inst, tasks, err := svc.Submit(ctx, tenant, input)
	if err != nil || len(tasks) != 1 {
		t.Fatal(inst, tasks, err)
	}
	if _, _, err := svc.Act(ctx, tenant, 500, tasks[0].ID, ActionApprove, ""); err == nil {
		t.Fatal("self approval")
	}
	accepted, next, err := svc.Act(ctx, tenant, 900, tasks[0].ID, ActionApprove, "")
	if err != nil || accepted.Status != statusApproved || len(next) != 0 {
		t.Fatal("extra approval stage", accepted, next, err)
	}
	again, _, err := svc.Submit(ctx, tenant, input)
	if err != nil || again.ID != inst.ID {
		t.Fatal("retry created another approval", again, err)
	}
	input.BizID = 2
	input.BizSummary = `{"approval_request_key":"return-one"}`
	_, tasks, err = svc.Submit(ctx, tenant, input)
	if err != nil {
		t.Fatal(err)
	}
	returned, _, err := svc.Act(ctx, tenant, 900, tasks[0].ID, ActionReject, "revise")
	if err != nil || returned.Status != statusReturned {
		t.Fatal("contract rejection must return", returned, err)
	}
	input.BizSummary = `{"approval_request_key":"return-two"}`
	resubmitted, _, err := svc.Submit(ctx, tenant, input)
	if err != nil || resubmitted.ID == returned.ID {
		t.Fatal("resubmission did not create new confirmation round", resubmitted, err)
	}
}
