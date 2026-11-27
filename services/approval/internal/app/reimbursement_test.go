package app

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/approval/internal/store"
)

type reimbursementDirectory struct {
	leader   int64
	roles    map[string][]int64
	managers []int64
}

func (d reimbursementDirectory) RoleMembers(context.Context, int64) ([]int64, error) { return nil, nil }
func (d reimbursementDirectory) RoleMembersByCode(_ context.Context, code string) ([]int64, bool, error) {
	v, ok := d.roles[code]
	return v, ok, nil
}
func (d reimbursementDirectory) ManagersOf(context.Context, int64, int32) ([]int64, error) {
	return d.managers, nil
}
func (d reimbursementDirectory) DepartmentLeaderOf(context.Context, int64) (int64, error) {
	return d.leader, nil
}
func TestTravelReimbursementApproversExcludeClaimant(t *testing.T) {
	ctx := context.Background()
	s := &Service{dir: reimbursementDirectory{leader: 20, roles: map[string][]int64{"FINANCE_MANAGER": {30, 10}, "BOSS": {40}}}}
	ids, err := s.assigneesFor(ctx, store.ApprovalNode{ApproverType: "DEPARTMENT_LEADER"}, 10)
	if err != nil || len(ids) != 1 || ids[0] != 20 {
		t.Fatalf("department leader ids=%v err=%v", ids, err)
	}
	ids, err = s.assigneesFor(ctx, store.ApprovalNode{ApproverType: "FINANCE_MANAGER"}, 10)
	if err != nil || len(ids) != 1 || ids[0] != 30 {
		t.Fatalf("finance ids=%v err=%v", ids, err)
	}
}
func TestFinanceManagerOwnClaimFallsBackToBoss(t *testing.T) {
	s := &Service{dir: reimbursementDirectory{leader: 20, roles: map[string][]int64{"FINANCE_MANAGER": {10}, "BOSS": {40}}}}
	ids, err := s.assigneesFor(context.Background(), store.ApprovalNode{ApproverType: "FINANCE_MANAGER"}, 10)
	if err != nil || len(ids) != 1 || ids[0] != 40 {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
}

type directReimbursementTestDirectory struct {
	reimbursementDirectory
	members []int64
}

func (d *directReimbursementTestDirectory) DirectReimbursementDepartment(_ context.Context, employeeID int64) (bool, error) {
	return employeeID >= 101 && employeeID <= 103, nil
}

func (d *directReimbursementTestDirectory) ReimbursementApprovers(context.Context) ([]int64, error) {
	return d.members, nil
}

func TestDirectReimbursementSharedApproval(t *testing.T) {
	ctx := context.Background()
	dir := &directReimbursementTestDirectory{
		reimbursementDirectory: reimbursementDirectory{leader: 301, roles: map[string][]int64{"SUPER_ADMIN": {101, 204}}},
		members:                []int64{101, 102, 201, 202, 204},
	}
	svc, cleanup := newSeedTestService(t, dir)
	tenant := time.Now().UnixNano()
	t.Cleanup(func() { cleanup(tenant) })

	for i, department := range []string{"海运部", "采购部", "质检部"} {
		submitter := int64(101 + i)
		_, tasks, err := svc.Submit(ctx, tenant, SubmitInput{
			BizType: reimbursementBizType, BizID: submitter, BizNo: department,
			SubmitterID: submitter, Amount: "100",
		})
		if err != nil || len(tasks) != 1 || tasks[0].NodeName != "财务审批" {
			t.Fatalf("%s tasks=%v err=%v", department, tasks, err)
		}
	}
	legacy, tasks, err := svc.Submit(ctx, tenant, SubmitInput{
		BizType: reimbursementBizType, BizID: 110, SubmitterID: 110, Amount: "100",
		BizSummary: `{"direct_finance_approval":true}`,
	})
	if err != nil || len(tasks) != 1 || tasks[0].AssigneeID != 301 || string(legacy.BizSummary) != "{}" {
		t.Fatalf("legacy routing: tasks=%v summary=%s err=%v", tasks, legacy.BizSummary, err)
	}
	task, _, _, err := svc.ActionableTask(ctx, tenant, 201, reimbursementBizType, 101)
	if err != nil || task.ID == 0 {
		t.Fatalf("actionable task=%v err=%v", task, err)
	}
	if ownTask, _, _, err := svc.ActionableTask(ctx, tenant, 101, reimbursementBizType, 101); err != nil || ownTask.ID != task.ID {
		t.Fatalf("super administrator's own actionable task: %v err=%v", ownTask, err)
	}
	adminQueue, _, err := svc.MyTasks(ctx, tenant, 101, reimbursementBizType, "", "", 1, 20)
	if err != nil || !slices.ContainsFunc(adminQueue, func(row store.ListMyTasksRow) bool { return row.BizID == 101 }) {
		t.Fatalf("super administrator's own claim missing from queue: %v err=%v", adminQueue, err)
	}
	ordinaryTask, _, _, err := svc.ActionableTask(ctx, tenant, 201, reimbursementBizType, 102)
	if err != nil || ordinaryTask.ID == 0 {
		t.Fatalf("ordinary applicant's claim missing from finance queue: %v err=%v", ordinaryTask, err)
	}
	if ownTask, _, _, err := svc.ActionableTask(ctx, tenant, 102, reimbursementBizType, 102); err != nil || ownTask.ID != 0 {
		t.Fatalf("ordinary applicant unexpectedly has approval action: %v err=%v", ownTask, err)
	}
	if _, _, err := svc.Act(ctx, tenant, 102, ordinaryTask.ID, ActionApprove, ""); apierr.CodeFromError(err) != "AP_REIMBURSEMENT_SELF" {
		t.Fatalf("self approval: %v", err)
	}
	for _, actor := range []int64{201, 202, 204} {
		queue, _, err := svc.MyTasks(ctx, tenant, actor, reimbursementBizType, "", "", 1, 20)
		count := 0
		for _, row := range queue {
			if row.NodeName == "财务审批" {
				count++
			}
		}
		if err != nil || count != 3 {
			t.Fatalf("actor %d sees %d direct tasks, err=%v", actor, count, err)
		}
	}

	// Update the active grants while the same three claims are still pending.
	dir.members = []int64{101, 202, 203, 204}
	if _, _, err := svc.Act(ctx, tenant, 201, task.ID, ActionApprove, ""); apierr.CodeFromError(err) != "AP_REIMBURSEMENT_APPROVAL_FORBIDDEN" {
		t.Fatalf("ungranted actor: %v", err)
	}
	ids, err := svc.MyInvolvedDocuments(ctx, tenant, 203, reimbursementBizType)
	if err != nil || len(ids) != 3 {
		t.Fatalf("newly granted actor: ids=%v err=%v", ids, err)
	}
	results := make(chan error, 2)
	for _, actor := range []int64{202, 203} {
		go func() {
			_, _, err := svc.Act(ctx, tenant, actor, task.ID, ActionApprove, "")
			results <- err
		}()
	}
	first, second := <-results, <-results
	if (first == nil) == (second == nil) {
		t.Fatalf("expected one approval, got %v and %v", first, second)
	}
	if first != nil && apierr.CodeFromError(first) != "AP_INSTANCE_FINISHED" ||
		second != nil && apierr.CodeFromError(second) != "AP_INSTANCE_FINISHED" {
		t.Fatalf("unexpected concurrent responses: %v, %v", first, second)
	}
	instances, err := svc.ListInstances(ctx, tenant, reimbursementBizType, 101)
	if err != nil || len(instances) != 1 || instances[0].Status != statusApproved {
		t.Fatalf("instances=%v err=%v", instances, err)
	}
	_, handled, err := svc.GetInstance(ctx, tenant, instances[0].ID)
	if err != nil || len(handled) != 1 || (handled[0].AssigneeID != 202 && handled[0].AssigneeID != 203) {
		t.Fatalf("actual approver not recorded: %v err=%v", handled, err)
	}
	for _, actor := range []int64{202, 203, 204} {
		if pending, _, _, err := svc.ActionableTask(ctx, tenant, actor, reimbursementBizType, 101); err != nil || pending.ID != 0 {
			t.Fatalf("actor %d still has processed claim pending: %v err=%v", actor, pending, err)
		}
	}
	task, _, _, err = svc.ActionableTask(ctx, tenant, 204, reimbursementBizType, 102)
	if err != nil || task.ID == 0 {
		t.Fatalf("return task=%v err=%v", task, err)
	}
	returned, _, err := svc.Act(ctx, tenant, 204, task.ID, ActionReturn, "补充凭证")
	if err != nil || returned.Status != statusReturned {
		t.Fatalf("return status=%q err=%v", returned.Status, err)
	}
	for _, actor := range []int64{202, 203, 204} {
		queue, _, err := svc.MyTasks(ctx, tenant, actor, reimbursementBizType, "", "", 1, 20)
		var direct []int64
		for _, row := range queue {
			if row.NodeName == "财务审批" {
				direct = append(direct, row.BizID)
			}
		}
		if err != nil || len(direct) != 1 || direct[0] != 103 {
			t.Fatalf("actor %d queue after approval and return: %v err=%v", actor, queue, err)
		}
	}
	dir.members = []int64{103}
	_, _, err = svc.Submit(ctx, tenant, SubmitInput{BizType: reimbursementBizType, BizID: 999, SubmitterID: 103, Amount: "100"})
	if apierr.CodeFromError(err) != "AP_REIMBURSEMENT_APPROVER_MISSING" {
		t.Fatalf("sole eligible approver is submitter: %v", err)
	}
	dir.members = []int64{101}
	_, tasks, err = svc.Submit(ctx, tenant, SubmitInput{BizType: reimbursementBizType, BizID: 998, SubmitterID: 101, Amount: "100"})
	if err != nil || len(tasks) != 1 {
		t.Fatalf("super administrator is sole eligible approver: tasks=%v err=%v", tasks, err)
	}
	approved, _, err := svc.Act(ctx, tenant, 101, tasks[0].ID, ActionApprove, "")
	if err != nil || approved.Status != statusApproved {
		t.Fatalf("super administrator self approval: status=%q err=%v", approved.Status, err)
	}
}
