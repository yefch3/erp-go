package app

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 智能转换的每月额度。
//
// 钉的是这套东西最容易做反的两条：
//
//  1. **没设额度 = 不限。** 一个我们没承诺过的默认上限突然开始拦人，比
//     不拦更糟——客户会在毫无预告的情况下发现功能坏了。
//  2. **上限 0 = 一次都不许用。** 和「不限」正好相反。如果哪天有人图省事
//     把「不限」重新表示成 0，这条会当场变红。
//  3. **额度是每人每月的，不是全公司的**（2026-09-01 改的）。见下面那条
//     单独的测试。
//
// 外加一条：失败的那次也占额度。模型答了钱就花了，不占额度等于给了一条
// 「一直失败就能无限用」的路。
func TestExcelQuota(t *testing.T) {
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	tenantID := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_excel_jobs WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_excel_quotas WHERE tenant_id=$1", tenantID)
	})

	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	const alice int64 = 701

	// 本月发起一次转换，不管成没成——成败都花钱，都占额度。
	run := func(status string) {
		if _, err := pool.Exec(ctx, `INSERT INTO mail_excel_jobs
			(tenant_id, owner_id, inbound_id, selected_text, status)
			VALUES ($1,$3,1,'选中的一段',$2)`, tenantID, status, alice); err != nil {
			t.Fatal(err)
		}
	}

	// ---- 没设额度：不限，怎么用都不拦 ----
	run("COMPLETED")
	run("FAILED")
	quota, err := svc.ExcelQuotaFor(ctx, tenantID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if quota.Limited {
		t.Fatal("没给这家公司设过额度，却报告说有上限——凭空的上限会在毫无预告的情况下拦住客户")
	}
	if quota.UsedThisMonth != 2 {
		t.Fatalf("本月已用 = %d，该是 2（成功一次 + 失败一次，失败也花了钱）", quota.UsedThisMonth)
	}
	if quota.Exhausted() {
		t.Fatal("不限额度却报告已用完")
	}
	if err := svc.ensureExcelQuota(ctx, tenantID, alice); err != nil {
		t.Fatalf("不限额度不该拦人：%v", err)
	}
	// 和**数据库**认定的当月比，不和 Go 的 UTC 时钟比。
	//
	// CurrentMonth 的用处是让前端拿它去和用量表里的月份对——而那些月份是
	// 数据库按 created_at 算的。所以这里真正该成立的是「它等于此刻写进去的
	// 一行会被归到的那个月」，而不是「它等于本机 UTC 的月份」。
	// 数据库时区一旦不是 UTC，后者在每个月最后几个小时必然不成立：
	// 2026-08-31 22:58 UTC 的 CI 就是这么红的。
	if want := dbCurrentMonth(t, ctx, pool); quota.CurrentMonth != want {
		t.Errorf("当前月份 = %q，数据库说是 %q", quota.CurrentMonth, want)
	}

	// ---- 上限比已用高：放行，并且百分比算得出来 ----
	if err := svc.SetExcelQuota(ctx, tenantID, true, 5, 99); err != nil {
		t.Fatal(err)
	}
	quota, err = svc.ExcelQuotaFor(ctx, tenantID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if !quota.Limited || quota.MonthlyRuns != 5 || quota.UsedThisMonth != 2 {
		t.Fatalf("额度状况不对：%+v，该是上限 5、已用 2", quota)
	}
	if err := svc.ensureExcelQuota(ctx, tenantID, alice); err != nil {
		t.Fatalf("2/5 还远没到顶，不该拦：%v", err)
	}

	// ---- 用到顶：拦住，而且话要说得清楚 ----
	run("COMPLETED")
	run("COMPLETED")
	run("FAILED") // 第 5 次，失败的那次同样占额度
	err = svc.ensureExcelQuota(ctx, tenantID, alice)
	if err == nil {
		t.Fatal("已用 5/5 却仍然放行——额度形同虚设")
	}
	if !strings.Contains(err.Error(), "5") {
		t.Errorf("拦人的话里没有具体数字，用户不知道自己用了多少：%q", err.Error())
	}

	// ---- 上限 0：一次都不许用。和「不限」正好相反 ----
	if err := svc.SetExcelQuota(ctx, tenantID, true, 0, 99); err != nil {
		t.Fatal(err)
	}
	if err := svc.ensureExcelQuota(ctx, tenantID, alice); err == nil {
		t.Fatal("上限设成 0 却还放行——0 被当成了「不限」，而它的意思正好相反")
	}

	// ---- 恢复不限：删掉那一行，不是把上限改成 0 ----
	if err := svc.SetExcelQuota(ctx, tenantID, false, 0, 99); err != nil {
		t.Fatal(err)
	}
	quota, err = svc.ExcelQuotaFor(ctx, tenantID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if quota.Limited {
		t.Fatal("恢复不限之后仍然报告有上限")
	}
	if err := svc.ensureExcelQuota(ctx, tenantID, alice); err != nil {
		t.Fatalf("恢复不限之后不该再拦：%v", err)
	}
}

// 上个月用满了，这个月要能重新开始。
//
// 「每月额度」四个字里，「每月」和「额度」一样要紧：如果计数从开天辟地累
// 计，那不是月度额度，是终身额度——客户用满一次就永远用不了了。
func TestExcelQuotaResetsEachMonth(t *testing.T) {
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	tenantID := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_excel_jobs WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_excel_quotas WHERE tenant_id=$1", tenantID)
	})

	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	const bob int64 = 702

	// 上个月用掉 3 次。日期由数据库自己算，不在 Go 里拼——月初那几个小时
	// 两个时钟差一点点，这条测试就会莫名其妙地红。
	for i := 0; i < 3; i++ {
		if _, err := pool.Exec(ctx, `INSERT INTO mail_excel_jobs
			(tenant_id, owner_id, inbound_id, selected_text, status, created_at)
			VALUES ($1,$2,1,'上个月的','COMPLETED', date_trunc('month', now()) - interval '5 days')`,
			tenantID, bob); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.SetExcelQuota(ctx, tenantID, true, 3, 99); err != nil {
		t.Fatal(err)
	}

	quota, err := svc.ExcelQuotaFor(ctx, tenantID, bob)
	if err != nil {
		t.Fatal(err)
	}
	if quota.UsedThisMonth != 0 {
		t.Fatalf("本月已用 = %d，该是 0——上个月的用量不该算进这个月", quota.UsedThisMonth)
	}
	if err := svc.ensureExcelQuota(ctx, tenantID, bob); err != nil {
		t.Fatalf("上个月用满了不该拖累这个月：%v", err)
	}
}

// 平台那一侧看到的是成本：本月烧了多少 token、折成多少钱。
//
// 钉两条：**没配单价就说没配**（空串，不是 0——一个凭空的 0 看着像账），
// 以及跨租户这条列表里能找到我们自己那一行且数字对得上。
func TestListExcelQuotasCarriesCostForThePlatform(t *testing.T) {
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	tenantID := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_excel_jobs WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_excel_quotas WHERE tenant_id=$1", tenantID)
	})

	if _, err := pool.Exec(ctx, `INSERT INTO mail_excel_jobs
		(tenant_id, owner_id, inbound_id, selected_text, status, input_tokens, output_tokens)
		VALUES ($1,703,1,'选中的一段','COMPLETED',120000,30000)`, tenantID); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	find := func(rows []TenantExcelQuota) TenantExcelQuota {
		t.Helper()
		for _, r := range rows {
			if r.TenantID == tenantID {
				return r
			}
		}
		t.Fatal("平台列表里找不到这家公司——用过就该出现，哪怕没设过额度")
		return TenantExcelQuota{}
	}

	// ---- 没配单价：token 照出，金额留空 ----
	rows, month, err := New(pool, Deps{}, log).ListExcelQuotas(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if month == "" {
		t.Error("没告诉调用方这是哪个月")
	}
	got := find(rows)
	if got.InputTokens != 120000 || got.OutputTokens != 30000 {
		t.Fatalf("token 数不对：%+v", got)
	}
	if got.EstimatedCost != "" {
		t.Fatalf("没配单价却给出了金额 %q——一个猜出来的成本比没有更坏", got.EstimatedCost)
	}

	// ---- 配了单价：按每百万 token 折算 ----
	// 12 万输入 × 1.25/百万 = 0.15；3 万输出 × 10/百万 = 0.30；合计 0.45。
	priced := New(pool, Deps{Pricing: ModelPricing{
		InputPerMTok:  decimal.RequireFromString("1.25"),
		OutputPerMTok: decimal.RequireFromString("10"),
		Currency:      "USD",
	}}, log)
	rows, _, err = priced.ListExcelQuotas(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got = find(rows)
	if got.EstimatedCost != "0.4500" || got.Currency != "USD" {
		t.Fatalf("金额折算错了：%q %q，该是 USD 0.4500", got.Currency, got.EstimatedCost)
	}
}

// 设了额度但这个月一次没转的公司，成本该是「本月 0 元」而不是「未配单价」。
//
// 上一版把金额只在「本月有用量」那个循环里赋值，于是这几家拿到空串，平台页
// 按空串渲染成「未配单价」——而单价明明配着。**空是「不知道」，0 是「不要
// 钱」**：把一个真实为 0 的数说成不知道，正是这套代码自己反复写下的那条规矩
// 被违反的样子。每个月 1 号整张表都会是这句假话。
func TestZeroUsageStillGetsACostWhenPricingIsConfigured(t *testing.T) {
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	tenantID := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_excel_quotas WHERE tenant_id=$1", tenantID)
	})

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	priced := New(pool, Deps{Pricing: ModelPricing{
		InputPerMTok:  decimal.RequireFromString("1.25"),
		OutputPerMTok: decimal.RequireFromString("10"),
		Currency:      "USD",
	}}, log)

	// 有额度，本月一次没转。
	if err := priced.SetExcelQuota(ctx, tenantID, true, 200, 99); err != nil {
		t.Fatal(err)
	}
	rows, _, err := priced.ListExcelQuotas(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got *TenantExcelQuota
	for i := range rows {
		if rows[i].TenantID == tenantID {
			got = &rows[i]
			break
		}
	}
	if got == nil {
		t.Fatal("设了额度的公司没出现在平台列表里")
	}
	if got.UsedThisMonth != 0 {
		t.Fatalf("本月已用 = %d，该是 0", got.UsedThisMonth)
	}
	if got.EstimatedCost != "0.0000" || got.Currency != "USD" {
		t.Fatalf("成本 = %q %q，该是 USD 0.0000。空串会让平台页显示「未配单价」"+
			"——而单价配着，真实答案是「本月 0 元」", got.Currency, got.EstimatedCost)
	}
}

// 只配了一半单价，就当没配。
//
// 上一版 Configured() 是 or：只要有一个价是正数就开始算钱，另一个按 0 参与
// 折算——等于宣布那一半的 token 免费，算出来的数会少一大截，而它看着和一笔
// 正确的账一模一样。更糟的是启动日志这时说「will be reported without a
// cost」，实际照样出了金额：一个自相矛盾的承诺意味着没人会去查。
func TestHalfConfiguredPricingProducesNoCostAtAll(t *testing.T) {
	full := ModelPricing{
		InputPerMTok:  decimal.RequireFromString("1.25"),
		OutputPerMTok: decimal.RequireFromString("10"),
		Currency:      "USD",
	}
	if !full.Configured() {
		t.Fatal("两个价都填了却说没配")
	}

	onlyInput := ModelPricing{InputPerMTok: decimal.RequireFromString("1.25"), Currency: "USD"}
	if onlyInput.Configured() {
		t.Fatal("只填了输入价却说配好了——输出 token 会被当成免费，账少算一大截，" +
			"而它看着和一笔正确的账一模一样")
	}

	onlyOutput := ModelPricing{OutputPerMTok: decimal.RequireFromString("10"), Currency: "USD"}
	if onlyOutput.Configured() {
		t.Fatal("只填了输出价却说配好了")
	}

	if (ModelPricing{}).Configured() {
		t.Fatal("什么都没填却说配好了")
	}
}

// 额度是**每人**的，一个人用完不牵连同事。
//
// 这是 2026-09-01 那次口径反转的全部内容，也是它唯一会静默做反的地方：
// CountExcelRunsThisMonth 少带一个 owner_id 就退回「按公司算」，而两个参数
// 都是 int64，**编译器一声不吭**。
//
// 做反了的样子：公司里有人上午跑满了额度，别人下午一点就被挡住，而挡人的
// 那句话说的是「你本月的额度已用完」——被挡的人自己一次都没用过，既看不出
// 是谁用掉的，也做不了任何事。
func TestQuotaIsPerPersonNotPerCompany(t *testing.T) {
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	tenantID := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_excel_jobs WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_excel_quotas WHERE tenant_id=$1", tenantID)
	})

	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	// 同一家公司的两个人。
	const heavy, light int64 = 711, 712

	// 每人每月 2 次。
	if err := svc.SetExcelQuota(ctx, tenantID, true, 2, 99); err != nil {
		t.Fatal(err)
	}
	// 其中一个人跑满。
	for i := 0; i < 2; i++ {
		if _, err := pool.Exec(ctx, `INSERT INTO mail_excel_jobs
			(tenant_id, owner_id, inbound_id, selected_text, status)
			VALUES ($1,$2,1,'选中的一段','COMPLETED')`, tenantID, heavy); err != nil {
			t.Fatal(err)
		}
	}

	// 跑满的那个人被挡住。
	err = svc.ensureExcelQuota(ctx, tenantID, heavy)
	if err == nil {
		t.Fatal("用满 2/2 的人还能继续转")
	}
	if !strings.Contains(err.Error(), "你本月") {
		t.Errorf("拦人的话没说清是「你的」额度：%q——按人算的额度说成公司的，"+
			"会让人去问同事是不是用超了，而那和他没关系", err.Error())
	}

	// **同事一次都没用过，不该被牵连。**
	quota, err := svc.ExcelQuotaFor(ctx, tenantID, light)
	if err != nil {
		t.Fatal(err)
	}
	if quota.UsedThisMonth != 0 {
		t.Fatalf("同事本月已用 = %d，该是 0——他一次都没转过。"+
			"这个数被算成了全公司的用量", quota.UsedThisMonth)
	}
	if quota.Exhausted() {
		t.Fatal("同事一次都没用过却被判成额度用尽")
	}
	if err := svc.ensureExcelQuota(ctx, tenantID, light); err != nil {
		t.Fatalf("同事被别人的用量挡住了：%v", err)
	}

	// 上限本身仍然是每人一样的那个数，从公司那一行来。
	if !quota.Limited || quota.MonthlyRuns != 2 {
		t.Fatalf("同事看到的上限 = %+v，该是「有上限、每人 2 次」", quota)
	}
}
