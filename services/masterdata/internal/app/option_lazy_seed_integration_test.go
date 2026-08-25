package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 新公司打开客户、供应商、报价页面，下拉框里就该有东西。
//
// 下拉字典的种子全部写死 tenant_id = 1（00001/00008/00009/00015），第二家起
// 的公司一个选项都没有：付款方式、贸易术语、客户类型、供应商类型全是空的。
// 和编码规则（#222）、审批流（#223）同一个病，但这一处最安静——空下拉框不
// 报错，人只会以为是自己没找对地方。
func TestOptionsSeedDefaultsForANewTenant(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool)

	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM option_items WHERE tenant_id=$1", tenantID)
	}()

	// ---- 全新公司，第一次打开报价页要付款方式 ----
	items, err := svc.ListOptions(ctx, tenantID, "PAYMENT_METHOD")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("新公司的付款方式该有 4 个选项，实际 %d 个", len(items))
	}
	if items[0].Code != "TT" || items[0].Label != "电汇 T/T" {
		t.Fatalf("默认值要和第一家公司逐字一致，实际 %s/%s", items[0].Code, items[0].Label)
	}

	// ---- 不带类别时把所有类别都补上 ----
	all, err := svc.ListOptions(ctx, tenantID, "")
	if err != nil {
		t.Fatal(err)
	}
	cats := map[string]int{}
	for _, it := range all {
		cats[it.Category]++
	}
	want := map[string]int{
		"PAYMENT_METHOD": 4, "TRADE_TERM": 5, "SHIPPING_EXCEPTION": 3,
		"CUSTOMER_TYPE": 5, "CUSTOMER_SOURCE": 5, "CUSTOMER_OWNER_RESPONSIBILITY": 5,
		"SUPPLIER_BUSINESS_TYPE": 6, "SUPPLIER_OWNER_RESPONSIBILITY": 4,
		"FACTORY_OWNER_RESPONSIBILITY": 4,
	}
	for cat, n := range want {
		if cats[cat] != n {
			t.Fatalf("类别 %s 该有 %d 个选项，实际 %d 个", cat, n, cats[cat])
		}
	}

	// ---- 人改过的名称永远赢：默认值只填空，不还原 ----
	if _, err := pool.Exec(ctx,
		`UPDATE option_items SET label='电汇（美元）' WHERE tenant_id=$1 AND category='PAYMENT_METHOD' AND code='TT'`,
		tenantID); err != nil {
		t.Fatal(err)
	}
	again, err := svc.ListOptions(ctx, tenantID, "PAYMENT_METHOD")
	if err != nil {
		t.Fatal(err)
	}
	if again[0].Label != "电汇（美元）" {
		t.Fatalf("人改过的名称被默认值还原了：%s", again[0].Label)
	}

	// ---- 不认识的类别照旧返回空，且不写任何东西 ----
	none, err := svc.ListOptions(ctx, tenantID, "NO_SUCH_CATEGORY")
	if err != nil {
		t.Fatalf("不认识的类别不该报错：%v", err)
	}
	if len(none) != 0 {
		t.Fatalf("凭空造出了 %d 个选项", len(none))
	}
}

// 人特意停用的选项不能被「补默认值」复活。
//
// 一个被去掉的付款方式如果每次打开页面都自己回来，它迟早会重新出现在发给
// 客户的合同上——而没有人会觉得那是系统干的。
func TestDeactivatedOptionsAreNotResurrected(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool)

	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM option_items WHERE tenant_id=$1", tenantID)
	}()

	if _, err := svc.ListOptions(ctx, tenantID, "PAYMENT_METHOD"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE option_items SET status='INACTIVE' WHERE tenant_id=$1 AND category='PAYMENT_METHOD'`,
		tenantID); err != nil {
		t.Fatal(err)
	}

	items, err := svc.ListOptions(ctx, tenantID, "PAYMENT_METHOD")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("停用的选项又出现在下拉框里了：%d 个", len(items))
	}
	var total int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM option_items WHERE tenant_id=$1 AND category='PAYMENT_METHOD'`,
		tenantID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Fatalf("停用之后又被播了一遍种，现在有 %d 条", total)
	}
}
