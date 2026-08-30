package app

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 对账页上的凭证。需求原话是「供应商发票只是员工用来上传留凭证用的」，
// 发票页下线之后这里是唯一的去处。
//
// 这一组钉的是四件事，按「出事之后有多难查」排：
//
//  1. **前缀检查是共用一个存储桶时唯一的跨租户隔离。** 没有它，任何人都能
//     把别人租户的对象登记到自己的采购单上，再从自己的页面把它读出来——
//     全程没有一行报错。这是这个文件里最要紧的一条。
//  2. 一张单挂多份，而不是一列存一份：一张单的纸是分几次到手的。
//  3. 撤下来是软删——「传错了」和「从来没传过」是两件事。
//  4. presign 什么都不写库：传到一半放弃的上传不该在采购单上留痕迹。

func reconFileTestPool(t *testing.T) (context.Context, *Service, *fakeFiles, int64, func()) {
	t.Helper()
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed recon file test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	files := &fakeFiles{}
	svc := New(pool, Deps{Files: files})
	cleanup := func() {
		for _, table := range []string{
			"purchase_order_recon_files", "purchase_order_payment_closures",
			"payment_allocations", "supplier_payments",
			"purchase_order_items", "purchase_orders",
		} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenantID)
		}
		pool.Close()
	}
	return ctx, svc, files, tenantID, cleanup
}

func TestReconFileLifecycle(t *testing.T) {
	ctx, svc, files, tenantID, cleanup := reconFileTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	poID := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RF-1", "5000", "ORDERED")

	// presign 什么都不写库：传到一半放弃，采购单上不该留下任何痕迹。
	p, err := svc.PresignReconFile(ctx, tenantID, poID, "8月发票.pdf", op)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.Key, "po-recon/") || p.UploadURL == "" {
		t.Fatalf("要一个 po-recon/ 前缀的 key 和一个上传地址，实际 %+v", p)
	}
	if got, err := svc.ListReconFiles(ctx, tenantID, poID, op); err != nil || len(got) != 0 {
		t.Fatalf("签完地址还没登记，列表必须是空的，实际 %d 条 err=%v", len(got), err)
	}

	// 登记之后才算数。
	list, err := svc.AttachReconFile(ctx, tenantID, poID, p.Key, "8月发票.pdf", "厂里开的票", op)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].FileName != "8月发票.pdf" || list[0].Note != "厂里开的票" {
		t.Fatalf("登记之后应有一份带备注的凭证，实际 %+v", list)
	}
	if list[0].URL == "" || list[0].UploadedByName != "Finance" {
		t.Fatalf("读的时候要现签地址、要带上传人，实际 %+v", list[0])
	}

	// **一张单挂多份**，不是一列存一份——第二次上传不该把第一份顶掉。
	p2, err := svc.PresignReconFile(ctx, tenantID, poID, "水单.jpg", op)
	if err != nil {
		t.Fatal(err)
	}
	list, err = svc.AttachReconFile(ctx, tenantID, poID, p2.Key, "水单.jpg", "", op)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("一张采购单要挂得住好几份纸，实际 %d 份", len(list))
	}
	if len(files.removed) != 0 {
		t.Fatalf("多传一份不是替换，不该删任何对象，实际删了 %v", files.removed)
	}

	// 撤下来是软删：列表里没了，行还在，谁撤的查得到。
	list, err = svc.RemoveReconFile(ctx, tenantID, list[0].ID, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].FileName != "水单.jpg" {
		t.Fatalf("撤掉第一份之后应只剩水单，实际 %+v", list)
	}
	var removedBy string
	if err := svc.pool.QueryRow(ctx, `
		SELECT removed_by_name FROM purchase_order_recon_files
		 WHERE tenant_id=$1 AND po_id=$2 AND removed_at IS NOT NULL`,
		tenantID, poID).Scan(&removedBy); err != nil {
		t.Fatalf("撤下来的那一行必须还在库里：%v", err)
	}
	if removedBy != "Finance" {
		t.Fatalf("要记下是谁撤的，实际 %q", removedBy)
	}
	// 对象也别删：撤错了还能补救，桶里的垃圾将来扫一遍就能收走，
	// 账上的空白补不回来。
	if len(files.removed) != 0 {
		t.Fatalf("撤下凭证不该删对象，实际删了 %v", files.removed)
	}
	// 撤两次是「没有可撤的」，不是崩。
	_, err = svc.RemoveReconFile(ctx, tenantID, list[0].ID+9999, op)
	wantAllocErr(t, err, "PR_FILE_NOT_FOUND")
}

// **这一条是这个文件存在的主要理由。**
//
// 所有服务共用一个对象存储桶，key 的前缀（po-recon/{租户}/{采购单}/）是唯一
// 把租户隔开的东西。attach 那一步如果不校验前缀，任何人都能把别人租户的对象
// 登记到自己的采购单上，然后从自己的页面上把它读出来——全程没有一行报错。
func TestReconFileKeyPrefixIsTheOnlyTenantFence(t *testing.T) {
	ctx, svc, _, tenantID, cleanup := reconFileTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	poID := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RF-FENCE", "1000", "ORDERED")
	otherPO := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RF-OTHER", "1000", "ORDERED")

	good, err := svc.PresignReconFile(ctx, tenantID, poID, "a.pdf", op)
	if err != nil {
		t.Fatal(err)
	}
	for name, key := range map[string]string{
		"别的租户":     "po-recon/999999/1/deadbeef-a.pdf",
		"同租户别的采购单": "po-recon/" + strconv.FormatInt(tenantID, 10) + "/" + strconv.FormatInt(otherPO, 10) + "/deadbeef-a.pdf",
		"别的模块的桶目录": "supplier-invoices/" + strconv.FormatInt(tenantID, 10) + "/1/deadbeef-a.pdf",
		"空 key":    "",
		// 前缀是字符串比较，所以「以合法前缀开头但爬出去了」也得挡住：
		// po-recon/{t}/{po}/../../999999/x 展开之后指向别人的目录。
		"路径穿越": good.Key[:strings.LastIndex(good.Key, "/")+1] + "../../999999/x.pdf",
	} {
		_, err := svc.AttachReconFile(ctx, tenantID, poID, key, "a.pdf", "", op)
		if err == nil {
			t.Errorf("%s：这个 key 必须被拒绝，实际放行了（key=%q）", name, key)
		}
	}
	// 自己签出来的那个照常放行。
	if _, err := svc.AttachReconFile(ctx, tenantID, poID, good.Key, "a.pdf", "", op); err != nil {
		t.Fatalf("自己签的 key 应当放行：%v", err)
	}
}

// 数据范围：按人截断的凭证列表是静默错误。和对账页其它接口同一个立场——
// 拿不到全量就明说，不悄悄缩水。
func TestReconFilesRefuseToShrinkQuietly(t *testing.T) {
	ctx, svc, _, tenantID, cleanup := reconFileTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	poID := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RF-SCOPE", "1000", "ORDERED")

	limited := New(svc.pool, Deps{Files: &fakeFiles{}, Scopes: fixedScope{Visibility{
		All: false, EmployeeIDs: []int64{77}, ScopeType: "SELF",
	}}})
	_, err := limited.ListReconFiles(ctx, tenantID, poID, op)
	wantAllocErr(t, err, "PR_RECON_SCOPE_LIMITED")
	_, err = limited.PresignReconFile(ctx, tenantID, poID, "a.pdf", op)
	wantAllocErr(t, err, "PR_RECON_SCOPE_LIMITED")
	_, err = limited.RemoveReconFile(ctx, tenantID, 1, op)
	wantAllocErr(t, err, "PR_RECON_SCOPE_LIMITED")
}

// 文件存储没配的时候要说人话，不是 nil panic。
func TestReconFilesWithoutStorageSayWhy(t *testing.T) {
	ctx, svc, _, tenantID, cleanup := reconFileTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	poID := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RF-NOSTORE", "1000", "ORDERED")

	bare := New(svc.pool, Deps{})
	_, err := bare.PresignReconFile(ctx, tenantID, poID, "a.pdf", op)
	wantAllocErr(t, err, "PR_FILES_UNAVAILABLE")
	// 读那一头相反：签不出地址是小事，为它让整个列表报错是大事。
	if _, err := bare.ListReconFiles(ctx, tenantID, poID, op); err != nil {
		t.Fatalf("没有文件存储也要能列出凭证（URL 留空即可）：%v", err)
	}
}
