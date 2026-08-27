package app

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/product/internal/store"
)

// 批量取和单条取必须返回**一模一样**的东西。
//
// 两条路各有一份 SQL 和一份到 proto 的映射。少映一个字段，批量那条路上就会
// 悄悄丢一列——而单条那条路是好的，所以查起来会很久：同一个产品，从合同页
// 进去有 HS 编码，从报价页进去没有。
//
// 另外两条同样要紧：
//
//   - **停用的产品也要返回**。在这里筛掉，调用方那句「产品已停用，不能报价」
//     就会变成「产品不存在」——一句让人去查产品主数据、结果什么也查不到的错话。
//   - **查不到的 id 就是不在结果里**，不报错。谁少了由调用方说，只有它知道
//     那是第几行。
func TestGetProductsMatchesGetProduct(t *testing.T) {
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
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM uoms WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM product_categories WHERE tenant_id=$1", tenantID)
	}()

	// 触发这家公司的默认单位和分类播种。
	uoms, err := svc.ListUoms(ctx, tenantID)
	if err != nil || len(uoms) == 0 {
		t.Fatalf("拿不到默认单位: %v", err)
	}
	cats, err := svc.ListCategories(ctx, tenantID, "")
	if err != nil || len(cats) == 0 {
		t.Fatalf("拿不到默认分类: %v", err)
	}

	mk := func(code, name, hs, status string) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO products
			(tenant_id, code, name, category_id, base_uom_id, hs_code, status,
			 reference_currency, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'USD',1,1) RETURNING id`,
			tenantID, code, name, cats[0].ID, uoms[0].ID, hs, status).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	active := mk("BATCH-A", "在用的", "7210.49", "ACTIVE")
	dead := mk("BATCH-B", "停用的", "7225.30", "INACTIVE")

	rows, err := svc.GetProducts(ctx, tenantID, []int64{active, dead, active + 999_999})
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	// **停用的也在里面**，查不到的那个不在。
	if len(rows) != 2 {
		t.Fatalf("要 2 条（停用的也算），实际 %d 条", len(rows))
	}
	got := map[int64]bool{}
	for _, r := range rows {
		got[r.ID] = true
	}
	if !got[active] || !got[dead] {
		t.Fatalf("在用的和停用的都该在，实际 %v", got)
	}

	// 逐字段和单条取对一遍。
	//
	// 用反射而不是手写字段列表：手写的那一刻就开始烂——以后谁给产品加一列，
	// 只在一条路上加，这个测试还是绿的。反射比的是「所有字段」，新列自动
	// 进入覆盖范围。
	for _, id := range []int64{active, dead} {
		one, err := svc.GetProduct(ctx, tenantID, id)
		if err != nil {
			t.Fatalf("GetProduct(%d): %v", id, err)
		}
		var many *store.GetProductsRow
		for i := range rows {
			if rows[i].ID == id {
				many = &rows[i]
			}
		}
		if many == nil {
			t.Fatalf("批量结果里少了产品 %d", id)
		}

		sv := reflect.ValueOf(one)
		mv := reflect.ValueOf(*many)
		st := sv.Type()
		if mv.Type().NumField() != st.NumField() {
			t.Fatalf("两个行类型字段数都对不上（单条 %d，批量 %d）——"+
				"其中一条路加了列而另一条没加",
				st.NumField(), mv.Type().NumField())
		}
		for i := 0; i < st.NumField(); i++ {
			name := st.Field(i).Name
			mf := mv.FieldByName(name)
			if !mf.IsValid() {
				t.Errorf("批量那条路没有字段 %s", name)
				continue
			}
			a := fmt.Sprintf("%v", sv.Field(i).Interface())
			b := fmt.Sprintf("%v", mf.Interface())
			if a != b {
				t.Errorf("产品 %d 的 %s 两条路不一致：单条=%q 批量=%q", id, name, a, b)
			}
		}
	}

	// 一个 id 都不给的时候不该去查库，也不该报错。
	empty, err := svc.GetProducts(ctx, tenantID, nil)
	if err != nil {
		t.Fatalf("空列表不该报错: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("空列表该返回空，实际 %d 条", len(empty))
	}
}
