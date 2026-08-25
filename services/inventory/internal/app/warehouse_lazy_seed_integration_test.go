package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 新公司第一次打开仓库列表，该有一个能用的仓库。
//
// 第一家公司安装时就得到了「主仓库」（00001_stock.sql 里 tenant_id 写死 1），
// 之后开的公司一个都没有。补种放在读列表的时候而不是开户的时候：不囤货的贸易
// 公司走的是直发，全程不需要仓库，不该因为开了个户就凭空多出一个仓库。
func TestWarehouseSeedsOnFirstLook(t *testing.T) {
	dsn := os.Getenv("INVENTORY_TEST_DSN")
	if dsn == "" {
		t.Skip("INVENTORY_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool, nil)

	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM warehouses WHERE tenant_id=$1", tenantID)
	}()

	rows, err := svc.ListWarehouses(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Code != "WH01" || rows[0].Name != "主仓库" {
		t.Fatalf("新公司第一次看仓库列表该有一个主仓库，实际 %+v", rows)
	}

	// 再看一次不会再补。
	again, err := svc.ListWarehouses(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 {
		t.Fatalf("第二次读又补了一个，现在有 %d 个仓库", len(again))
	}
}

// 已经自己建过仓库的公司，不该被默认值多塞一个。
func TestWarehouseSeedDoesNotAddToATenantThatHasItsOwn(t *testing.T) {
	dsn := os.Getenv("INVENTORY_TEST_DSN")
	if dsn == "" {
		t.Skip("INVENTORY_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool, nil)

	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM warehouses WHERE tenant_id=$1", tenantID)
	}()

	if _, err := pool.Exec(ctx,
		`INSERT INTO warehouses (tenant_id, code, name, wh_type) VALUES ($1,'NB01','宁波保税仓','BONDED')`,
		tenantID); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ListWarehouses(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Code != "NB01" {
		t.Fatalf("自己建过仓库的公司被塞进了默认仓库：%+v", rows)
	}
}

// 人特意停用的仓库不该被补种变回来：货会被收进一个公司认为已经不用了的地方。
func TestDeactivatedWarehousesAreNotResurrected(t *testing.T) {
	dsn := os.Getenv("INVENTORY_TEST_DSN")
	if dsn == "" {
		t.Skip("INVENTORY_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool, nil)

	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM warehouses WHERE tenant_id=$1", tenantID)
	}()

	if _, err := svc.ListWarehouses(ctx, tenantID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		"UPDATE warehouses SET status='INACTIVE' WHERE tenant_id=$1", tenantID); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ListWarehouses(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("停用的仓库又回来了：%+v", rows)
	}
	var total int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM warehouses WHERE tenant_id=$1", tenantID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("停用之后又被播了一遍种，现在有 %d 条", total)
	}
}
