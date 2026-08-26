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

	// 本月发起一次转换，不管成没成——成败都花钱，都占额度。
	run := func(status string) {
		if _, err := pool.Exec(ctx, `INSERT INTO mail_excel_jobs
			(tenant_id, owner_id, inbound_id, selected_text, status)
			VALUES ($1,701,1,'选中的一段',$2)`, tenantID, status); err != nil {
			t.Fatal(err)
		}
	}

	// ---- 没设额度：不限，怎么用都不拦 ----
	run("COMPLETED")
	run("FAILED")
	quota, err := svc.ExcelQuotaFor(ctx, tenantID)
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
	if err := svc.ensureExcelQuota(ctx, tenantID); err != nil {
		t.Fatalf("不限额度不该拦人：%v", err)
	}
	if quota.CurrentMonth != time.Now().UTC().Format("2006-01") {
		t.Errorf("当前月份 = %q，和本机 UTC 当月对不上", quota.CurrentMonth)
	}

	// ---- 上限比已用高：放行，并且百分比算得出来 ----
	if err := svc.SetExcelQuota(ctx, tenantID, true, 5, 99); err != nil {
		t.Fatal(err)
	}
	quota, err = svc.ExcelQuotaFor(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if !quota.Limited || quota.MonthlyRuns != 5 || quota.UsedThisMonth != 2 {
		t.Fatalf("额度状况不对：%+v，该是上限 5、已用 2", quota)
	}
	if err := svc.ensureExcelQuota(ctx, tenantID); err != nil {
		t.Fatalf("2/5 还远没到顶，不该拦：%v", err)
	}

	// ---- 用到顶：拦住，而且话要说得清楚 ----
	run("COMPLETED")
	run("COMPLETED")
	run("FAILED") // 第 5 次，失败的那次同样占额度
	err = svc.ensureExcelQuota(ctx, tenantID)
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
	if err := svc.ensureExcelQuota(ctx, tenantID); err == nil {
		t.Fatal("上限设成 0 却还放行——0 被当成了「不限」，而它的意思正好相反")
	}

	// ---- 恢复不限：删掉那一行，不是把上限改成 0 ----
	if err := svc.SetExcelQuota(ctx, tenantID, false, 0, 99); err != nil {
		t.Fatal(err)
	}
	quota, err = svc.ExcelQuotaFor(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if quota.Limited {
		t.Fatal("恢复不限之后仍然报告有上限")
	}
	if err := svc.ensureExcelQuota(ctx, tenantID); err != nil {
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

	// 上个月用掉 3 次。日期由数据库自己算，不在 Go 里拼——月初那几个小时
	// 两个时钟差一点点，这条测试就会莫名其妙地红。
	for i := 0; i < 3; i++ {
		if _, err := pool.Exec(ctx, `INSERT INTO mail_excel_jobs
			(tenant_id, owner_id, inbound_id, selected_text, status, created_at)
			VALUES ($1,702,1,'上个月的','COMPLETED', date_trunc('month', now()) - interval '5 days')`,
			tenantID); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.SetExcelQuota(ctx, tenantID, true, 3, 99); err != nil {
		t.Fatal(err)
	}

	quota, err := svc.ExcelQuotaFor(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if quota.UsedThisMonth != 0 {
		t.Fatalf("本月已用 = %d，该是 0——上个月的用量不该算进这个月", quota.UsedThisMonth)
	}
	if err := svc.ensureExcelQuota(ctx, tenantID); err != nil {
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
