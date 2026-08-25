package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 新公司提交第一份合同、第一张采购单，就该走得起来——不该撞「该单据类型未
// 配置审批流」。
//
// 审批流的种子全部写死 tenant_id = 1（00001/00005/00006），第二家起的公司一
// 条都没有。和编码规则（#222）、邮件后台只服务第一家（#216）是同一个病。
// 修法同样是读侧懒播种：没有流程且类型认识，就补一套再走。
type stubDirectory struct {
	// 按层级给上级：1 是直属上级，2 是上级的上级。缺的层级返回空，
	// 表示汇报线到顶了。
	managers map[int32][]int64
	// 角色成员，按角色**编码**。第二家公司没有这个角色时是空的。
	roles  map[int64][]int64
	byCode map[string][]int64
}

func (d stubDirectory) RoleMembers(_ context.Context, roleID int64) ([]int64, error) {
	return d.roles[roleID], nil
}

func (d stubDirectory) RoleMembersByCode(_ context.Context, code string) ([]int64, bool, error) {
	ids, ok := d.byCode[code]
	return ids, ok, nil
}

func (d stubDirectory) ManagersOf(_ context.Context, _ int64, levels int32) ([]int64, error) {
	return d.managers[levels], nil
}

func newSeedTestService(t *testing.T, dir Directory) (*Service, func(int64)) {
	t.Helper()
	dsn := os.Getenv("APPROVAL_TEST_DSN")
	if dsn == "" {
		t.Skip("set APPROVAL_TEST_DSN to a migrated PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	// 采购兜底角色沿用默认编码。
	svc := New(pool, dir, nil, "PROCUREMENT_MANAGER")
	cleanup := func(tenantID int64) {
		for _, stmt := range []string{
			"DELETE FROM approval_tasks WHERE tenant_id=$1",
			"DELETE FROM outbox_events WHERE tenant_id=$1",
			"DELETE FROM approval_instances WHERE tenant_id=$1",
			"DELETE FROM approval_nodes WHERE tenant_id=$1",
			"DELETE FROM approval_definitions WHERE tenant_id=$1",
		} {
			if _, err := pool.Exec(ctx, stmt, tenantID); err != nil {
				t.Errorf("cleanup %q: %v", stmt, err)
			}
		}
	}
	return svc, cleanup
}

func TestNewTenantGetsDefaultApprovalFlows(t *testing.T) {
	dir := stubDirectory{managers: map[int32][]int64{1: {900}, 2: {901}}}
	svc, cleanup := newSeedTestService(t, dir)
	ctx := context.Background()

	tenantID := time.Now().UnixNano()
	t.Cleanup(func() { cleanup(tenantID) })

	// ---- 合同：第二家公司提交第一份合同 ----
	inst, tasks, err := svc.Submit(ctx, tenantID, SubmitInput{
		BizType: "CONTRACT", BizID: 1, BizNo: "CT-0001",
		SubmitterID: 500, SubmitterName: "小王", Amount: "1000",
	})
	if err != nil {
		t.Fatalf("新公司提交第一份合同就该走得起来，实际：%v", err)
	}
	if inst.Status != statusRunning {
		t.Fatalf("合同应进入审批中，实际 %s", inst.Status)
	}
	if len(tasks) != 1 || tasks[0].AssigneeID != 900 || tasks[0].NodeName != "直属上级审批" {
		t.Fatalf("第一个节点该落到直属上级头上，实际 %+v", tasks)
	}

	// ---- 采购单大额档：金额要真的选到那一档 ----
	_, poTasks, err := svc.Submit(ctx, tenantID, SubmitInput{
		BizType: "PURCHASE_ORDER", BizID: 2, BizNo: "PO-0001",
		SubmitterID: 500, SubmitterName: "小王", Amount: "60000",
	})
	if err != nil {
		t.Fatalf("新公司提交第一张采购单就该走得起来，实际：%v", err)
	}
	if len(poTasks) != 1 || poTasks[0].AssigneeID != 900 {
		t.Fatalf("采购单第一个节点该落到直属上级头上，实际 %+v", poTasks)
	}
	defs, err := svc.ListDefinitions(ctx, tenantID, "")
	if err != nil {
		t.Fatal(err)
	}
	var bands []string
	for _, d := range defs {
		if d.BizType == "PURCHASE_ORDER" {
			bands = append(bands, d.Name+"@"+d.MinAmount)
		}
	}
	if len(bands) != 2 {
		t.Fatalf("采购单该按金额分两档，实际 %v", bands)
	}

	// ---- 播种只发生一次：再提交不会长出第二套 ----
	if _, _, err := svc.Submit(ctx, tenantID, SubmitInput{
		BizType: "CONTRACT", BizID: 3, BizNo: "CT-0002",
		SubmitterID: 500, SubmitterName: "小王", Amount: "1000",
	}); err != nil {
		t.Fatal(err)
	}
	after, err := svc.ListDefinitions(ctx, tenantID, "")
	if err != nil {
		t.Fatal(err)
	}
	contracts := 0
	for _, d := range after {
		if d.BizType == "CONTRACT" {
			contracts++
		}
	}
	if contracts != 1 {
		t.Fatalf("第二次提交又播了一套种，合同流程现在有 %d 条", contracts)
	}

	// ---- 不认识的单据类型照旧拒绝：写错的类型名不该造出一条真实流程 ----
	if _, _, err := svc.Submit(ctx, tenantID, SubmitInput{
		BizType: "CONTARCT", BizID: 4, BizNo: "X-1",
		SubmitterID: 500, SubmitterName: "小王",
	}); err == nil {
		t.Fatal("写错的类型名造出了审批流")
	}
}

// 人特意关掉的流程不能被「补默认值」复活。
//
// 一条都没有 = 没播过种；有但都停用了 = 有人做过决定。只看 ACTIVE 会把后者
// 当成前者，于是每提交一次就把人家关掉的审批复活一次——而审批被绕过是没人
// 会立刻发现的那种错。
func TestDisabledFlowIsNotResurrected(t *testing.T) {
	dir := stubDirectory{managers: map[int32][]int64{1: {900}}}
	svc, cleanup := newSeedTestService(t, dir)
	ctx := context.Background()

	tenantID := time.Now().UnixNano()
	t.Cleanup(func() { cleanup(tenantID) })

	if _, _, err := svc.Submit(ctx, tenantID, SubmitInput{
		BizType: "CONTRACT", BizID: 1, BizNo: "CT-0001",
		SubmitterID: 500, SubmitterName: "小王", Amount: "1000",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.pool.Exec(ctx,
		"UPDATE approval_definitions SET status='INACTIVE' WHERE tenant_id=$1", tenantID); err != nil {
		t.Fatal(err)
	}

	_, _, err := svc.Submit(ctx, tenantID, SubmitInput{
		BizType: "CONTRACT", BizID: 2, BizNo: "CT-0002",
		SubmitterID: 500, SubmitterName: "小王", Amount: "1000",
	})
	if err == nil {
		t.Fatal("停用的审批流被默认值复活了")
	}
	if !strings.Contains(err.Error(), "未配置审批流") {
		t.Fatalf("该说的是没有可用审批流，实际：%v", err)
	}
}

// 采购单是真金白银的承诺：没人可批时必须停下来，不能自动通过。
//
// 这条同时钉住了播种的边界——补出流程不等于补出审批人。第二家公司的兜底
// 角色编号属于第一家，成员必然为空，这里拿到的应当是一句能照着做的拒绝，
// 而不是一张悄悄自动通过的采购单。
func TestPurchaseOrderNeverAutoApprovesWithoutApprovers(t *testing.T) {
	dir := stubDirectory{} // 一个人的公司：没有上级，兜底角色也没有成员
	svc, cleanup := newSeedTestService(t, dir)
	ctx := context.Background()

	tenantID := time.Now().UnixNano()
	t.Cleanup(func() { cleanup(tenantID) })

	_, _, err := svc.Submit(ctx, tenantID, SubmitInput{
		BizType: "PURCHASE_ORDER", BizID: 1, BizNo: "PO-0001",
		SubmitterID: 500, SubmitterName: "小王", Amount: "1000",
	})
	if err == nil {
		t.Fatal("没有任何审批人，采购单却自动通过了")
	}
	if !strings.Contains(err.Error(), "审批人") {
		t.Fatalf("该说的是没有可用审批人，实际：%v", err)
	}
	// 话要能照着做：角色不存在时，提示要说「创建或启用」。
	if !strings.Contains(err.Error(), "PROCUREMENT_MANAGER") {
		t.Fatalf("提示没说是哪个角色，人照着做不了：%v", err)
	}
	if !strings.Contains(err.Error(), "创建") {
		t.Fatalf("角色不存在时该让人去创建：%v", err)
	}
}

// 角色在、只是没人时，提示只该说「加人」。
//
// 预置角色现在开户即有（iam 那边自动播种），所以这是绝大多数公司会遇到的
// 那一种。让人去「创建」一个已经躺在角色页里的角色，他会找半天以为自己看错了。
func TestApproverMessageSaysAddMembersWhenTheRoleExists(t *testing.T) {
	// byCode 里有这个键但值为空 = 角色存在、里面没人。
	dir := stubDirectory{byCode: map[string][]int64{"PROCUREMENT_MANAGER": {}}}
	svc, cleanup := newSeedTestService(t, dir)
	ctx := context.Background()

	tenantID := time.Now().UnixNano()
	t.Cleanup(func() { cleanup(tenantID) })

	_, _, err := svc.Submit(ctx, tenantID, SubmitInput{
		BizType: "PURCHASE_ORDER", BizID: 1, BizNo: "PO-0001",
		SubmitterID: 500, SubmitterName: "小王", Amount: "1000",
	})
	if err == nil {
		t.Fatal("角色里一个人都没有，采购单却通过了")
	}
	if !strings.Contains(err.Error(), "添加成员") {
		t.Fatalf("角色已存在时该让人去加人：%v", err)
	}
	if strings.Contains(err.Error(), "创建") {
		t.Fatalf("角色明明在角色页里躺着，却让人去创建——人会找半天以为看错了：%v", err)
	}
}

// 兜底审批角色按**编码**找，不按编号。
//
// 编号属于某一家公司：写死一个数字，第二家公司永远找不到那个角色，而提示还
// 让他去「配置采购审批角色成员」——他公司里根本没有那个角色。按编码找，每家
// 公司都能解析到自己的那一个。
func TestPurchaseFallbackIsResolvedByRoleCodeNotID(t *testing.T) {
	dir := stubDirectory{byCode: map[string][]int64{"PROCUREMENT_MANAGER": {777}}}
	svc, cleanup := newSeedTestService(t, dir)
	ctx := context.Background()

	tenantID := time.Now().UnixNano()
	t.Cleanup(func() { cleanup(tenantID) })

	// 提交人没有上级，走兜底：任务该落到本公司采购经理头上。
	_, tasks, err := svc.Submit(ctx, tenantID, SubmitInput{
		BizType: "PURCHASE_ORDER", BizID: 1, BizNo: "PO-0001",
		SubmitterID: 500, SubmitterName: "小王", Amount: "1000",
	})
	if err != nil {
		t.Fatalf("本公司有采购经理，采购单就该走得起来：%v", err)
	}
	if len(tasks) != 1 || tasks[0].AssigneeID != 777 {
		t.Fatalf("兜底任务该落到本公司采购经理头上，实际 %+v", tasks)
	}
}

// 合同没有上级可批时，记录在案地自动通过——和第一家公司的行为一致。
func TestContractApprovesWhenTheReportingLineRunsOut(t *testing.T) {
	dir := stubDirectory{}
	svc, cleanup := newSeedTestService(t, dir)
	ctx := context.Background()

	tenantID := time.Now().UnixNano()
	t.Cleanup(func() { cleanup(tenantID) })

	inst, tasks, err := svc.Submit(ctx, tenantID, SubmitInput{
		BizType: "CONTRACT", BizID: 1, BizNo: "CT-0001",
		SubmitterID: 500, SubmitterName: "老板", Amount: "1000",
	})
	if err != nil {
		t.Fatalf("一个人的公司也该能提交合同，实际：%v", err)
	}
	if inst.Status != statusApproved || len(tasks) != 0 {
		t.Fatalf("提交人之上没人可问时该记录在案地通过，实际 %s / %d 条待办", inst.Status, len(tasks))
	}
}
