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

func TestD2PrivilegedSubmitterCanConfirmOwnContract(t *testing.T) {
	for _, tc := range []struct {
		name     string
		roleCode string
	}{
		{name: "销售经理", roleCode: salesManagerRoleCode},
		{name: "超级管理员", roleCode: superAdminRoleCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := stubDirectory{byCode: map[string][]int64{tc.roleCode: {500}}}
			svc, cleanup := newSeedTestService(t, dir)
			tenant := time.Now().UnixNano()
			defer cleanup(tenant)

			inst, tasks, err := svc.Submit(context.Background(), tenant, SubmitInput{
				BizType: "CONTRACT", BizID: 1, BizNo: "CT-SELF-CONFIRM",
				SubmitterID: 500, SubmitterName: tc.name, Amount: "1000",
			})
			if err != nil {
				t.Fatalf("有确认权限的提交人应能提交自己的合同: %v", err)
			}
			if inst.Status != statusRunning || len(tasks) != 1 || tasks[0].AssigneeID != 500 {
				t.Fatalf("应生成一条由提交人手动处理的确认任务: inst=%+v tasks=%+v", inst, tasks)
			}

			approved, next, err := svc.Act(context.Background(), tenant, 500, tasks[0].ID, ActionApprove, "本人确认")
			if err != nil {
				t.Fatalf("有确认权限的提交人应能确认自己的合同: %v", err)
			}
			if approved.Status != statusApproved || len(next) != 0 {
				t.Fatalf("本人确认后合同审批应结束: approved=%+v next=%+v", approved, next)
			}
		})
	}
}
