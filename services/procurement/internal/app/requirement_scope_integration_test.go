package app

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// A1 收尾的钉子：需求归合同负责人，围栏之下别人的合同拆出的需求
// 既不列出、也解析成「不存在」；手工需求归创建人；属主未知（0）的
// 历史行只有「全部」范围能看见。
func TestRequirementDataScope(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed scope test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1`, tenantID)
	}()

	const salesA, salesB, admin = 81, 82, 83
	stub := &scopeStub{views: map[int64]Visibility{
		salesA: {EmployeeIDs: []int64{salesA}, ScopeType: "SELF"},
		salesB: {EmployeeIDs: []int64{salesB}, ScopeType: "SELF"},
		admin:  {All: true, ScopeType: "ALL"},
	}}
	svc := New(pool, Deps{Scopes: stub})
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// 两个销售各一张生效合同：属主随事件落到需求上。
	mkContract := func(contractID int64, no string, owner int64, ownerName string) {
		if err := svc.RequirementsFromContract(ctx, tenantID, ContractEffective{
			ContractID: contractID, ContractNo: no, VersionID: contractID*10 + 1, VersionNo: 1,
			CustomerName: "客户" + ownerName, SalesEmployeeID: owner, SalesEmployee: ownerName,
			Items: []ContractLine{{ItemID: contractID*100 + 1, ProductID: 11, ProductCode: "P-11",
				ProductName: "Scope Coil " + no, Qty: "10", UomID: 7, UomCode: "TON"}},
		}, log, noopClaim); err != nil {
			t.Fatal(err)
		}
	}
	mkContract(9001, "CT-SCOPE-A", salesA, "SalesA")
	mkContract(9002, "CT-SCOPE-B", salesB, "SalesB")
	// 属主未知的历史行。
	var legacyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO purchase_requirements (tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,status) VALUES ($1,9003,'CT-SCOPE-LEGACY',90031,900301,11,'P-11','Legacy Coil',7,'TON',10,'PENDING') RETURNING id`, tenantID).Scan(&legacyID); err != nil {
		t.Fatal(err)
	}

	opA := Operator{ID: salesA, Name: "SalesA"}
	opAdmin := Operator{ID: admin, Name: "Admin"}

	// SELF：只看见自己合同的需求，历史行也不漏。
	rows, total, err := svc.ListRequirements(ctx, tenantID, RequirementFilter{}, 1, 50, opA)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 || rows[0].ContractNo != "CT-SCOPE-A" || rows[0].OwnerName != "SalesA" {
		t.Fatalf("SELF list rows=%d total=%d first=%+v", len(rows), total, rows)
	}
	// ALL：三条全见。
	if _, total, err = svc.ListRequirements(ctx, tenantID, RequirementFilter{}, 1, 50, opAdmin); err != nil || total != 3 {
		t.Fatalf("ALL total=%d err=%v", total, err)
	}

	// 别人的需求按单寻址：答「不存在」。
	var bID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM purchase_requirements WHERE tenant_id=$1 AND contract_no='CT-SCOPE-B'`, tenantID).Scan(&bID); err != nil {
		t.Fatal(err)
	}
	if err := svc.AuthorizeRequirement(ctx, tenantID, bID, opA); code(err) != "PR_REQUIREMENT_NOT_FOUND" {
		t.Fatalf("expected not-found for foreign requirement, got %v", err)
	}
	// 历史行对 SELF 也是「不存在」，对 ALL 放行。
	if err := svc.AuthorizeRequirement(ctx, tenantID, legacyID, opA); code(err) != "PR_REQUIREMENT_NOT_FOUND" {
		t.Fatalf("expected not-found for ownerless legacy row, got %v", err)
	}
	if err := svc.AuthorizeRequirement(ctx, tenantID, legacyID, opAdmin); err != nil {
		t.Fatalf("ALL must see legacy rows: %v", err)
	}

	// 手工需求归创建人。
	manual, err := svc.CreateRequirement(ctx, tenantID, ManualRequirement{
		ProductID: 11, ProductCode: "P-11", ProductName: "Manual Coil",
		UomID: 7, UomCode: "TON", RequiredQty: "3",
	}, opA)
	if err != nil {
		t.Fatal(err)
	}
	if manual.OwnerID != salesA || manual.OwnerName != "SalesA" {
		t.Fatalf("manual owner=%d/%q", manual.OwnerID, manual.OwnerName)
	}

	// 合同重发不带属主（旧事件）：保留已知属主，不抹成未知。
	if err := svc.RequirementsFromContract(ctx, tenantID, ContractEffective{
		ContractID: 9001, ContractNo: "CT-SCOPE-A", VersionID: 90011, VersionNo: 1,
		CustomerName: "客户SalesA",
		Items: []ContractLine{{ItemID: 900101, ProductID: 11, ProductCode: "P-11",
			ProductName: "Scope Coil CT-SCOPE-A", Qty: "12", UomID: 7, UomCode: "TON"}},
	}, log, noopClaim); err != nil {
		t.Fatal(err)
	}
	var keptOwner int64
	if err := pool.QueryRow(ctx, `SELECT owner_id FROM purchase_requirements WHERE tenant_id=$1 AND contract_item_id=900101`, tenantID).Scan(&keptOwner); err != nil {
		t.Fatal(err)
	}
	if keptOwner != salesA {
		t.Fatalf("owner wiped by ownerless redelivery: %d", keptOwner)
	}
}
