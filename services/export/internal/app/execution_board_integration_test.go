package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 合同执行一览的出口半边（D2）：货走了多少、钱收了多少，都折成金额。
//
// 折算这一步是最容易悄悄算错的地方——按产品配对而不是按明细行，用生效版本
// 的加权均价，超出合同金额也不夹紧。四条都在这里钉住。
func TestContractExecutionBoard(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, tbl := range []string{
			"contract_shipments", "receipt_allocations", "bank_transactions", "bank_accounts",
			"contract_items", "contract_versions", "contracts",
		} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+tbl+" WHERE tenant_id=$1", tenantID)
		}
	}()

	const salesA, salesB, admin = 61, 62, 63

	// mkContract 造一张合同，版本先留在草稿——已批准的版本被触发器冻住，
	// 明细加不进去。加完明细再调 approve 落定。
	mkContract := func(no string, sales int64, status, total string) (int64, int64) {
		var id, versionID int64
		if err := pool.QueryRow(ctx, `INSERT INTO contracts
			(tenant_id, contract_no, customer_id, customer_name, status, sales_employee_id, sales_employee,
			 effective_at, created_by, updated_by)
			VALUES ($1,$2,9,'客户',$3,$4,'销售', now() - interval '3 days', 1, 1) RETURNING id`,
			tenantID, no, status, sales).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO contract_versions
			(tenant_id, contract_id, version_no, status, currency, total_amount, created_by,
			 fx_rate, fx_rate_at, fx_source)
			VALUES ($1,$2,1,'DRAFT','USD',$3::numeric,1, 1, now(), 'TEST') RETURNING id`,
			tenantID, id, total).Scan(&versionID); err != nil {
			t.Fatal(err)
		}
		return id, versionID
	}
	approve := func(contractID, versionID int64) {
		if _, err := pool.Exec(ctx, `UPDATE contract_versions SET status='APPROVED' WHERE id=$1`, versionID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE contracts SET current_version_id=$2 WHERE id=$1`, contractID, versionID); err != nil {
			t.Fatal(err)
		}
	}
	addItem := func(versionID int64, lineNo int32, productID int64, qty, unitPrice, amount string) {
		if _, err := pool.Exec(ctx, `INSERT INTO contract_items
			(tenant_id, contract_version_id, line_no, product_id, product_code, product_name,
			 qty, uom_code, unit_price, amount)
			VALUES ($1,$2,$3,$4,'P','产品',$5::numeric,'TON',$6::numeric,$7::numeric)`,
			tenantID, versionID, lineNo, productID, qty, unitPrice, amount); err != nil {
			t.Fatal(err)
		}
	}
	ship := func(contractID int64, outboundNo string, productID int64, qty string) {
		if _, err := pool.Exec(ctx, `INSERT INTO contract_shipments
			(tenant_id, contract_id, contract_item_id, product_id, sku_id, outbound_no, qty)
			VALUES ($1,$2,0,$3,0,$4,$5::numeric)`,
			tenantID, contractID, productID, outboundNo, qty); err != nil {
			t.Fatal(err)
		}
	}
	var acctID int64
	if err := pool.QueryRow(ctx, `INSERT INTO bank_accounts (tenant_id,account_no,account_name,currency)
		VALUES ($1,'ACC-EXEC','我方账户','USD') RETURNING id`, tenantID).Scan(&acctID); err != nil {
		t.Fatal(err)
	}
	collect := func(contractID int64, contractNo, ref, amount string) {
		var txnID int64
		if err := pool.QueryRow(ctx, `INSERT INTO bank_transactions
			(tenant_id,account_id,bank_ref,direction,amount,currency,value_date)
			VALUES ($1,$2,$3,'CREDIT',$4::numeric,'USD',current_date) RETURNING id`,
			tenantID, acctID, ref, amount).Scan(&txnID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO receipt_allocations
			(tenant_id,transaction_id,contract_id,contract_no,amount,currency)
			VALUES ($1,$2,$3,$4,$5::numeric,'USD')`,
			tenantID, txnID, contractID, contractNo, amount); err != nil {
			t.Fatal(err)
		}
	}

	// ① 走了一半货、收了三成款。两个产品，单价不同——按产品配对才算得对。
	halfID, halfVer := mkContract("CT-EXEC-HALF", salesA, "EXECUTING", "50000")
	addItem(halfVer, 1, 101, "100", "300", "30000") // 100 吨 × 300
	addItem(halfVer, 2, 102, "100", "200", "20000") // 100 件 × 200
	approve(halfID, halfVer)
	ship(halfID, "OB-1", 101, "50") // 50 × 300 = 15000
	ship(halfID, "OB-2", 102, "50") // 50 × 200 = 10000
	collect(halfID, "CT-EXEC-HALF", "REF-HALF", "15000")

	// ② 同一个产品落在两行、单价不同：折算要用加权均价，不能取其中一个。
	//    120 × 500 + 80 × 250 = 80000，共 200 件 → 均价 400。
	//    发了 100 件 → 40000。
	twoID, twoVer := mkContract("CT-EXEC-TWOLINE", salesA, "EXECUTING", "80000")
	addItem(twoVer, 1, 201, "120", "500", "60000")
	addItem(twoVer, 2, 201, "80", "250", "20000")
	approve(twoID, twoVer)
	ship(twoID, "OB-3", 201, "100")

	// ③ 发出去的比合同还多（改版把量调小而货已经发了）。不夹紧，露出来。
	overID, overVer := mkContract("CT-EXEC-OVER", salesA, "EXECUTING", "10000")
	addItem(overVer, 1, 301, "10", "1000", "10000")
	approve(overID, overVer)
	ship(overID, "OB-4", 301, "13") // 13 × 1000 = 13000 > 10000

	// ④ 一点没动的：还没发货、还没收款。
	approve(mkContract("CT-EXEC-IDLE", salesA, "EFFECTIVE", "7000"))

	// ⑤ 别人的合同，⑥ 还没签的草稿——前者被围栏挡住，后者被默认状态挡住。
	approve(mkContract("CT-EXEC-OTHER", salesB, "EXECUTING", "9000"))
	approve(mkContract("CT-EXEC-DRAFT", salesA, "DRAFT", "9000"))

	stub := &receivableScopeStub{views: map[int64]Visibility{
		salesA: {EmployeeIDs: []int64{salesA}, ScopeType: "SELF"},
		admin:  {All: true, ScopeType: "ALL"},
	}}
	svc := New(pool, Deps{Scopes: stub})

	rows, total, err := svc.ListContractExecution(ctx, tenantID, ExecutionFilter{}, 1, 50,
		Operator{ID: salesA, Name: "A"})
	if err != nil {
		t.Fatal(err)
	}
	byNo := map[string]ExecutionRow{}
	for _, r := range rows {
		byNo[r.ContractNo] = r
	}
	// 自己的四张在跑的合同：草稿不算（还没签就没有进程），别人的不算。
	if total != 4 || len(rows) != 4 {
		t.Fatalf("该看到自己的四张在跑的合同，实际 %d 张：%+v", total, rows)
	}
	if _, leaked := byNo["CT-EXEC-OTHER"]; leaked {
		t.Fatal("别人的合同越过了围栏")
	}
	if _, leaked := byNo["CT-EXEC-DRAFT"]; leaked {
		t.Fatal("草稿不该出现在执行一览上——还没签，没有进程可言")
	}

	half := byNo["CT-EXEC-HALF"]
	if half.ShippedAmount != "25000.00" {
		t.Fatalf("两个产品各发一半，已出运金额该是 25000，实际 %s", half.ShippedAmount)
	}
	if half.ReceivedAmount != "15000.00" {
		t.Fatalf("已收款该是 15000，实际 %s", half.ReceivedAmount)
	}
	if half.TotalAmount != "50000.00" {
		t.Fatalf("合同金额该是 50000，实际 %s", half.TotalAmount)
	}

	if two := byNo["CT-EXEC-TWOLINE"]; two.ShippedAmount != "40000.00" {
		t.Fatalf("同一产品跨两行该按加权均价 400 折算，100 件 = 40000，实际 %s", two.ShippedAmount)
	}

	// 超发不夹紧：13000 > 合同的 10000，露出来才有人去查为什么。
	if over := byNo["CT-EXEC-OVER"]; over.ShippedAmount != "13000.00" {
		t.Fatalf("超发该原样露出 13000，实际 %s", over.ShippedAmount)
	}

	// 一点没动的给零，不是 NULL——页面上「0」就是「还没开始」。
	idle := byNo["CT-EXEC-IDLE"]
	if idle.ShippedAmount != "0" || idle.ReceivedAmount != "0" {
		t.Fatalf("没发货没收款该是 0/0，实际 %s / %s", idle.ShippedAmount, idle.ReceivedAmount)
	}

	// ALL 范围看得见别人的那张。
	allRows, allTotal, err := svc.ListContractExecution(ctx, tenantID, ExecutionFilter{}, 1, 50,
		Operator{ID: admin, Name: "Admin"})
	if err != nil {
		t.Fatal(err)
	}
	if allTotal != 5 {
		t.Fatalf("ALL 该看到五张在跑的合同，实际 %d：%+v", allTotal, allRows)
	}

	// 显式指定状态时按状态查，不再套「只看在跑的」那层默认。
	drafts, draftTotal, err := svc.ListContractExecution(ctx, tenantID, ExecutionFilter{Status: "DRAFT"}, 1, 50,
		Operator{ID: salesA, Name: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if draftTotal != 1 || drafts[0].ContractNo != "CT-EXEC-DRAFT" {
		t.Fatalf("显式查草稿该只出那一张，实际 %d：%+v", draftTotal, drafts)
	}

	// 关键词落在合同号和客户名上。
	_, kwTotal, err := svc.ListContractExecution(ctx, tenantID, ExecutionFilter{Keyword: "EXEC-HALF"}, 1, 50,
		Operator{ID: salesA, Name: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if kwTotal != 1 {
		t.Fatalf("按合同号搜该只出一张，实际 %d", kwTotal)
	}
}
