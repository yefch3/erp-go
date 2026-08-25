package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 新公司要能建出第一个产品。
//
// 建产品时基本单位和分类都必选，而这两张表的种子全部写死 tenant_id = 1
// （00001_product.sql）。第二家起的公司一个单位都没有，于是一个产品都建不
// 出来——单位在界面上还没有新增入口，人在里面转一圈找不到任何出路。这是
// 「只给第一家公司播种」里最硬的一处。
func TestCatalogSeedsDefaultsForANewTenant(t *testing.T) {
	dsn := os.Getenv("PRODUCT_TEST_DSN")
	if dsn == "" {
		t.Skip("PRODUCT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool, nil, nil)

	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM uoms WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM product_categories WHERE tenant_id=$1", tenantID)
	}()

	// ---- 单位 ----
	uoms, err := svc.ListUoms(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(uoms) != 8 {
		t.Fatalf("新公司该有 8 个默认单位，实际 %d 个——一个都没有就建不出产品", len(uoms))
	}
	byCode := map[string]string{}
	for _, u := range uoms {
		byCode[u.Code] = u.Name + "/" + u.UomType
	}
	if byCode["PCS"] != "个/COUNT" || byCode["KG"] != "千克/WEIGHT" || byCode["CBM"] != "立方米/VOLUME" {
		t.Fatalf("默认单位要和第一家公司逐字一致，实际 %v", byCode)
	}

	// ---- 分类 ----
	cats, err := svc.ListCategories(ctx, tenantID, "ACTIVE")
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 1 || cats[0].Code != "DEFAULT" || cats[0].Name != "未分类" {
		t.Fatalf("新公司该有一条兜底分类，实际 %+v", cats)
	}
	// 路径和层级要和 CreateCategory 建根分类时算出来的一样，否则播出来的这条
	// 和人手建的不是同一种东西，子树查询会漏掉它。
	if cats[0].Path != "/" || cats[0].Level != 1 {
		t.Fatalf("兜底分类的路径/层级不对：%q / %d", cats[0].Path, cats[0].Level)
	}

	// ---- 再读一次不会再补：种子只播一次 ----
	if again, err := svc.ListUoms(ctx, tenantID); err != nil {
		t.Fatal(err)
	} else if len(again) != 8 {
		t.Fatalf("第二次读又补了一批，现在有 %d 个单位", len(again))
	}
}

// 已经自己建过单位的公司，不该被默认值灌一批进来。
//
// 「补默认值」补的是空白，不是「让每家公司长得一样」。一家只用吨和立方米的
// 公司，不该因为读了一次列表就多出「双」和「套」。
func TestCatalogDoesNotFloodATenantThatAlreadyHasItsOwn(t *testing.T) {
	dsn := os.Getenv("PRODUCT_TEST_DSN")
	if dsn == "" {
		t.Skip("PRODUCT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool, nil, nil)

	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM uoms WHERE tenant_id=$1", tenantID)
	}()

	if _, err := svc.CreateUom(ctx, tenantID, "BAG", "袋", "COUNT"); err != nil {
		t.Fatal(err)
	}
	uoms, err := svc.ListUoms(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(uoms) != 1 || uoms[0].Code != "BAG" {
		t.Fatalf("自己建过单位的公司被默认值灌进来了：%d 个", len(uoms))
	}
}

// 人特意停用的单位不能被「补默认值」复活。
func TestDeactivatedUomsAreNotResurrected(t *testing.T) {
	dsn := os.Getenv("PRODUCT_TEST_DSN")
	if dsn == "" {
		t.Skip("PRODUCT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool, nil, nil)

	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM uoms WHERE tenant_id=$1", tenantID)
	}()

	if _, err := svc.ListUoms(ctx, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		"UPDATE uoms SET status='INACTIVE' WHERE tenant_id=$1", tenantID); err != nil {
		t.Fatal(err)
	}

	uoms, err := svc.ListUoms(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(uoms) != 0 {
		t.Fatalf("停用的单位又回来了：%d 个", len(uoms))
	}
	var total int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM uoms WHERE tenant_id=$1", tenantID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 8 {
		t.Fatalf("停用之后又被播了一遍种，现在有 %d 条", total)
	}
}
