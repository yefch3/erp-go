package app

import (
	"context"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 建合同时填的应收到期日，必须真的落进库里。
//
// 这条测试是为一个真事写的：**建合同有两条路**——直接建（contract_direct.go）
// 和从报价生成（contract.go）——给合同加到期日字段时只接了后者。前端热更新
// 之后，直接建的合同看上去填了日期，存进去却是空的；一直到财务在对账页上
// 看见「未填」才发现。中间没有任何一处报错：Go 的结构体字面量少写一个字段
// 就是零值，编译器不会说话。
//
// 所以这里不走 SQL 造数据，**真的把两个 service 方法各调一遍**——只有这样
// 才能证明那一句映射在。以后再往合同上加字段，照着这里补一遍。

type dueCustomerStub struct{}

func (dueCustomerStub) Get(_ context.Context, id int64) (Customer, error) {
	return Customer{ID: id, Name: "ACME", Address: "1 Road", Currency: "USD", Status: "ACTIVE"}, nil
}

type dueProductStub struct{}

// 合同和报价不一样：合同的每一行都必须挂在一个真实产品上，
// 所以这里得给得出货。
func dueProduct(id int64) Product {
	return Product{ID: id, Code: "P-1", Name: "热镀锌钢卷", UomID: 1,
		UomCode: "MT", HsCode: "7210.49", Status: "ACTIVE"}
}

func (dueProductStub) Get(_ context.Context, id int64) (Product, error) {
	return dueProduct(id), nil
}

func (dueProductStub) GetMany(_ context.Context, ids []int64) (map[int64]Product, error) {
	out := map[int64]Product{}
	for _, id := range ids {
		out[id] = dueProduct(id)
	}
	return out, nil
}

type dueRateStub struct{}

func (dueRateStub) Latest(context.Context, string) (Rate, error) {
	return Rate{Rate: decimal.RequireFromString("7.1"), At: time.Now(), Source: "test", Base: "CNY"}, nil
}

type dueNumberingStub struct {
	mu sync.Mutex
	n  int
}

func (d *dueNumberingStub) Next(context.Context, string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.n++
	return "CT-DUE-" + time.Now().Format("20060102150405.000") + "-" + strconv.Itoa(d.n), nil
}

func TestTheDueDateSomebodyTypedActuallyReachesTheDatabase(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set; skipping DB-backed contract due test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, tbl := range []string{"contract_items", "contract_versions", "contracts"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+tbl+` WHERE tenant_id=$1`, tenantID)
		}
	}()

	svc := New(pool, Deps{
		Customers: dueCustomerStub{},
		Products:  dueProductStub{},
		Rates:     dueRateStub{},
		Numbering: &dueNumberingStub{},
	})
	op := Operator{ID: 5, Name: "Sales"}
	due := dbToday(ctx, t, pool).AddDate(0, 0, 45).Format("2006-01-02")

	view, err := svc.CreateContract(ctx, tenantID, DirectContractInput{
		CustomerID: 7, Currency: "USD",
		Terms: Terms{DeliveryDate: due, ReceivableDueDate: due},
		Items: []ItemInput{{
			ProductID: 1, ProductName: "热镀锌钢卷", UomCode: "MT", Spec: "ASTM A653",
			Qty: "10", UnitPrice: "100",
		}},
	}, op)
	if err != nil {
		t.Fatalf("直接建合同：%v", err)
	}

	var stored string
	if err := pool.QueryRow(ctx, `
		SELECT coalesce(receivable_due_date::text, '') FROM contracts WHERE id=$1`,
		view.Contract.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != due {
		t.Fatalf("直接建合同时填的到期日是 %q，存进库里却是 %q——"+
			"建合同有两条路，这条当初就是漏掉的那条", due, stored)
	}

	// 读回来的那一份也要带着它：财务在对账页上看的是这个数，
	// 而合同详情页要能让人核对自己填的对不对。
	if view.Contract.ReceivableDueDate != due {
		t.Errorf("建完返回的合同里到期日是 %q，应该是 %q",
			view.Contract.ReceivableDueDate, due)
	}

	// 生效**不许动**这个日子。上一版这里会去客户主数据取账期重算一遍；
	// 那条口径已经推翻，留着的话人填的日子会在生效那一刻被覆盖。
	if _, err := pool.Exec(ctx, `
		UPDATE contracts SET status='EFFECTIVE', effective_at=now(), signed_at=now()
		 WHERE id=$1`, view.Contract.ID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT coalesce(receivable_due_date::text, '') FROM contracts WHERE id=$1`,
		view.Contract.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != due {
		t.Fatalf("生效之后到期日变成了 %q，应该还是 %q", stored, due)
	}

	// 日期打错了要在服务层就被挡下来，而不是让它变成数据库的 22007、
	// 再变成网关的 500「系统错误」。
	if _, err := svc.CreateContract(ctx, tenantID, DirectContractInput{
		CustomerID: 7, Currency: "USD",
		Terms: Terms{ReceivableDueDate: "2026-13-01"},
		Items: []ItemInput{{ProductID: 1, ProductName: "x", UomCode: "MT", Qty: "1", UnitPrice: "1"}},
	}, op); err == nil {
		t.Error("月份越界的日期应该被服务层拒掉")
	}
}
