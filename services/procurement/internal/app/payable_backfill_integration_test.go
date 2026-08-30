package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 存量采购单补应付到期日。
//
// 要钉住的不是「补了几行」，而是补算**不许**做的两件事：
//
//  1. 没配账期的供应商，单子必须继续留空。空表示「没配账期」，不是
//     「当天到期」——底层 UPDATE 里 ordered_at::date + 0 正好等于下单
//     当天，一不留神就会把一堆单子写成「早就到期」。
//  2. 已经有到期日的行一个字都不能动。补算是给历史补课，不是重算。
//
// 外加一条会被人忘掉的：跳过的部分必须如实报数。只报「补了 37 张」而不
// 报「还有 5 家没配、涉及 12 张」，一次补了一半的操作看起来就像做完了。

func seedBackfillOrder(ctx context.Context, t *testing.T, pool *pgxpool.Pool,
	tenantID, supplierID int64, no string, orderedDaysAgo int, due string) int64 {
	t.Helper()
	var id int64
	// orderedDaysAgo 传负数 = 从没下过单（草稿那一类），ordered_at 留空。
	if err := pool.QueryRow(ctx, `
		INSERT INTO purchase_orders
		  (tenant_id, po_no, supplier_id, supplier_code, supplier_name, currency,
		   total_amount, expected_date, status, buyer_id, buyer_name,
		   ordered_at, payable_due_date)
		VALUES ($1, $2, $3, 'SUP', 'Mill', 'USD', 1000,
		        current_date + 10, 'ORDERED', 77, 'Buyer',
		        CASE WHEN $5::int < 0 THEN NULL
		             ELSE now() - make_interval(days => $5::int) END,
		        nullif($4::text, '')::date)
		RETURNING id`,
		tenantID, no, supplierID, due, orderedDaysAgo).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func payableDueOf(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id int64) (string, int32) {
	t.Helper()
	var due string
	var days int32
	if err := pool.QueryRow(ctx, `
		SELECT coalesce(payable_due_date::text, ''), payment_days
		  FROM purchase_orders WHERE id = $1`, id).Scan(&due, &days); err != nil {
		t.Fatal(err)
	}
	return due, days
}

func TestBackfillPayableDueUsesConfiguredTermsAndLeavesTheRestEmpty(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed backfill test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenantID)
	}()

	// 三家供应商，只有 11 号配了账期。12 号是「有单但没配」，13 号在主数据
	// 里根本查不到（停用/删掉），两者都必须被当成「没配」跳过，而不是让
	// 整次补算失败。
	svc := New(pool, Deps{Suppliers: supplierMapDirectoryStub{
		11: {ID: 11, Code: "SUP-11", Name: "Configured", Currency: "USD", Status: "ACTIVE", PaymentDays: 30},
		12: {ID: 12, Code: "SUP-12", Name: "Unset", Currency: "USD", Status: "ACTIVE", PaymentDays: 0},
	}})
	op := Operator{ID: 77, Name: "Finance"}

	blank := seedBackfillOrder(ctx, t, pool, tenantID, 11, "PO-BF-BLANK", 10, "")
	alsoBlank := seedBackfillOrder(ctx, t, pool, tenantID, 11, "PO-BF-BLANK2", 3, "")
	kept := seedBackfillOrder(ctx, t, pool, tenantID, 11, "PO-BF-KEPT", 10, "2020-01-01")
	noTerms := seedBackfillOrder(ctx, t, pool, tenantID, 12, "PO-BF-NOTERMS", 10, "")
	gone := seedBackfillOrder(ctx, t, pool, tenantID, 13, "PO-BF-GONE", 10, "")
	neverOrdered := seedBackfillOrder(ctx, t, pool, tenantID, 11, "PO-BF-DRAFT", -1, "")

	res, err := svc.BackfillPayableDue(ctx, tenantID, op)
	if err != nil {
		t.Fatal(err)
	}

	// 补上的两张：起点是**下单那天**，不是今天。10 天前下的单 + 30 天账期
	// = 20 天后到期；今天起算会变成 30 天后，差整整十天。
	if due, days := payableDueOf(ctx, t, pool, blank); due != today(ctx, t, pool, 20) || days != 30 {
		t.Errorf("10 天前下的单按 30 天账期应该 20 天后到期，得到 %q（账期 %d）", due, days)
	}
	if due, _ := payableDueOf(ctx, t, pool, alsoBlank); due != today(ctx, t, pool, 27) {
		t.Errorf("3 天前下的单应该 27 天后到期，得到 %q", due)
	}
	// 已经有日子的不动——补算是补课，不是重算。
	if due, _ := payableDueOf(ctx, t, pool, kept); due != "2020-01-01" {
		t.Errorf("已有到期日被补算改写成了 %q", due)
	}
	// 这三张必须继续空着。**空不等于当天到期**：写成下单当天，页面立刻
	// 会把它们报成「已逾期」，而其实只是没人配账期。
	for _, c := range []struct {
		id   int64
		what string
	}{
		{noTerms, "没配账期的供应商"},
		{gone, "主数据里查不到的供应商"},
		{neverOrdered, "从没下过单的行"},
	} {
		if due, _ := payableDueOf(ctx, t, pool, c.id); due != "" {
			t.Errorf("%s的单不该被补上到期日，却得到 %q", c.what, due)
		}
	}

	// 跳过的部分要如实报数，否则「补完了」是句假话。13 号供应商查不到，
	// 和 12 号一样算「没配」。
	if res.UpdatedOrders != 2 || res.AppliedSuppliers != 1 {
		t.Errorf("应该补 2 张单、涉及 1 家供应商，得到 %d 张 / %d 家",
			res.UpdatedOrders, res.AppliedSuppliers)
	}
	if res.SkippedSuppliers != 2 || res.SkippedOrders != 2 {
		t.Errorf("应该跳过 2 家供应商、2 张单，得到 %d 家 / %d 张",
			res.SkippedSuppliers, res.SkippedOrders)
	}

	// 幂等：再跑一遍不该有任何行被改。这条是「点两次没关系」的凭据——
	// 员工看到跳过的数字不为零，第一反应就是配好账期再点一次。
	again, err := svc.BackfillPayableDue(ctx, tenantID, op)
	if err != nil {
		t.Fatal(err)
	}
	if again.UpdatedOrders != 0 {
		t.Errorf("补算不幂等：第二遍又改了 %d 行", again.UpdatedOrders)
	}
	if again.SkippedOrders != 2 {
		t.Errorf("第二遍应该还剩 2 张没配账期的单，得到 %d", again.SkippedOrders)
	}
	if due, _ := payableDueOf(ctx, t, pool, blank); due != today(ctx, t, pool, 20) {
		t.Errorf("第二遍把已经补好的到期日改成了 %q", due)
	}
}

// today 用库里的 current_date 算，不用 Go 的时钟：到期日是 DATE，跨时区
// 比较会在午夜前后差一天，而这条测试要断言的是天数，不是时区。
func today(ctx context.Context, t *testing.T, pool *pgxpool.Pool, plusDays int) string {
	t.Helper()
	var out string
	if err := pool.QueryRow(ctx,
		`SELECT (current_date + $1::int)::text`, plusDays).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
