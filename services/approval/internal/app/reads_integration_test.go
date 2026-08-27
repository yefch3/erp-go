package app

import (
	"context"
	"testing"
	"time"
)

// HOME1 的个人查询必须同时守住两种关系：提交人只能看自己发起的实例，
// 审批人只能看分配给自己的任务。这里直接用两个员工编号证明它们不会串数据。
func TestHomeApprovalReadsStayPersonal(t *testing.T) {
	dir := stubDirectory{managers: map[int32][]int64{1: {900}}}
	svc, cleanup := newSeedTestService(t, dir)
	ctx := context.Background()
	tenantID := time.Now().UnixNano()
	t.Cleanup(func() { cleanup(tenantID) })

	if _, _, err := svc.Submit(ctx, tenantID, SubmitInput{
		BizType: "CONTRACT", BizID: 71, BizNo: "CT-HOME-0071",
		BizSummary:  `{"customer":"HOME 测试客户"}`,
		SubmitterID: 500, SubmitterName: "提交员工", Amount: "1000",
	}); err != nil {
		t.Fatal(err)
	}

	started, total, err := svc.MySubmitted(ctx, tenantID, 500,
		"CONTRACT", statusRunning, "HOME", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(started) != 1 || started[0].SubmitterID != 500 {
		t.Fatalf("本人发起查询结果不正确: total=%d rows=%+v", total, started)
	}

	other, otherTotal, err := svc.MySubmitted(ctx, tenantID, 501,
		"", "", "", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if otherTotal != 0 || len(other) != 0 {
		t.Fatalf("其他员工看到了提交人的审批实例: total=%d rows=%+v", otherTotal, other)
	}

	tasks, taskTotal, err := svc.MyTasks(ctx, tenantID, 900,
		"CONTRACT", "", "HOME", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if taskTotal != 1 || len(tasks) != 1 || tasks[0].AssigneeID != 900 {
		t.Fatalf("审批人个人待办结果不正确: total=%d rows=%+v", taskTotal, tasks)
	}

	wrongTasks, wrongTotal, err := svc.MyTasks(ctx, tenantID, 500,
		"", "", "", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if wrongTotal != 0 || len(wrongTasks) != 0 {
		t.Fatalf("提交人看到了未分配给自己的任务: total=%d rows=%+v", wrongTotal, wrongTasks)
	}
}
