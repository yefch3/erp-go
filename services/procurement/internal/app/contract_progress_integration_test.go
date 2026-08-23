package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 一页合同的采购进度（D2）：共几项、订齐几项、到齐几项。
//
// 三条容易搞错的边界钉在这里：作废和被改版顶掉的行不算；没有采购需求的
// 合同压根不出现（让网关补零）；一次问多张合同，各归各的。
func TestContractProcurementProgress(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM purchase_requirements WHERE tenant_id=$1", tenantID)
	}()

	line := func(contractID, itemID int64, required, ordered, received, status string) {
		if _, err := pool.Exec(ctx, `INSERT INTO purchase_requirements
			(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,
			 product_id,product_code,product_name,uom_id,uom_code,
			 required_qty,ordered_qty,received_qty,status)
			VALUES ($1,$2,'CT',1,$3,11,'P','产品',7,'TON',$4::numeric,$5::numeric,$6::numeric,$7)`,
			tenantID, contractID, itemID, required, ordered, received, status); err != nil {
			t.Fatal(err)
		}
	}

	const contractA, contractB, contractC = 7001, 7002, 7003
	// 合同 A：三项。一项到齐、一项订齐没到、一项刚订了一半。
	line(contractA, 1, "100", "100", "100", "RECEIVED")
	line(contractA, 2, "50", "50", "0", "ORDERED")
	line(contractA, 3, "80", "40", "0", "PARTIALLY_ORDERED")
	// 合同 A 还有两行不该算：作废的和被改版顶掉的。它们不是没办完的活，
	// 是不存在的活；算进去会让「共几项」凭空变多。
	line(contractA, 4, "60", "0", "0", "CANCELLED")
	line(contractA, 5, "70", "0", "0", "SUPERSEDED")
	// 合同 B：一项，一点没订。
	line(contractB, 6, "20", "0", "0", "PENDING")

	svc := New(pool, Deps{})
	got, err := svc.ContractProcurementProgress(ctx, tenantID, []int64{contractA, contractB, contractC})
	if err != nil {
		t.Fatal(err)
	}

	a := got[contractA]
	if a.TotalLines != 3 || a.OrderedLines != 2 || a.ReceivedLines != 1 {
		t.Fatalf("合同 A 该是 共 3 项 / 订齐 2 项 / 到齐 1 项，实际 %+v", a)
	}
	b := got[contractB]
	if b.TotalLines != 1 || b.OrderedLines != 0 || b.ReceivedLines != 0 {
		t.Fatalf("合同 B 该是 共 1 项 / 订齐 0 / 到齐 0，实际 %+v", b)
	}
	// 没有采购需求的合同不出现在结果里。网关按合同补零，这样「一项都没有」
	// 和「查不到」在页面上是同一个答案：还没开始采购。
	if _, present := got[contractC]; present {
		t.Fatalf("没有采购需求的合同不该出现在结果里，实际 %+v", got[contractC])
	}

	// 空输入不该打库。
	empty, err := svc.ContractProcurementProgress(ctx, tenantID, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("空输入该直接返回空，实际 %+v / %v", empty, err)
	}
}
