package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 银行流水那份对账单。
//
// 流水行是员工敲进来的数，对账单是银行出的凭证——两者对不上时说了算的是纸。
// 这条钉住四件事：
//
//	· 浏览器登记的 key 必须是**这一行流水**的 presign 才可能发出来的
//	  （别的流水不行、别的租户更不行——所有服务共用一个桶，前缀是唯一的隔离）
//	· 重传是替换：行先指向新的，旧对象随后被收走
//	· 读的时候现签下载地址，且**从不落库**（存下来必然是个过期链接）
//	· 附件和「这笔钱处理完没有」毫无关系——传了附件不会让流水行变成已认领
func TestBankTransactionAttachment(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed attachment test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM bank_transactions WHERE tenant_id=$1`, tenantID)
	}()

	files := &fakeFiles{}
	svc := New(pool, Deps{Files: files})
	op := Operator{ID: 77, Name: "Finance"}

	row, err := svc.RecordBankTransaction(ctx, tenantID, BankTransactionInput{
		BankRef: "ST-" + itoa64(tenantID), Direction: "CREDIT", Amount: "12000.00",
		Currency: "USD", TxnDate: "2026-08-25", Counterparty: "ACME",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if row.AttachmentKey != "" || row.AttachmentURL != "" {
		t.Fatalf("刚登记的流水不该有附件：%+v", row)
	}

	presign, err := svc.PresignBankTransactionFile(ctx, tenantID, row.ID, "2026-08 statement.pdf", op)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(presign.Key, bankTxnKeyPrefix(tenantID, row.ID)) {
		t.Fatalf("key 没按租户+流水分目录：%s", presign.Key)
	}
	// presign 只是发一张许可，什么都不该落库——传到一半放弃不能在行上留痕。
	if again, err := svc.GetBankTransaction(ctx, tenantID, row.ID, op); err != nil || again.AttachmentKey != "" {
		t.Fatalf("presign 之后行上不该有附件：%v %+v", err, again.AttachmentKey)
	}

	// 别人家的 key 登记不进来。这一道是唯一的跨租户隔离。
	for _, bad := range []string{
		bankTxnKeyPrefix(tenantID, row.ID+1) + "abcdef01-x.pdf", // 另一行流水
		bankTxnKeyPrefix(tenantID+1, row.ID) + "abcdef01-x.pdf", // 另一个租户
		"supplier-invoices/1/1/abcdef01-x.pdf",                  // 另一种单据
	} {
		if _, err := svc.AttachBankTransactionFile(ctx, tenantID, row.ID, bad, op); err == nil ||
			!strings.Contains(err.Error(), "BANK_FILE_KEY_MISMATCH") {
			t.Fatalf("不该被接受的 key %q，实际 %v", bad, err)
		}
	}

	got, err := svc.AttachBankTransactionFile(ctx, tenantID, row.ID, presign.Key, op)
	if err != nil {
		t.Fatal(err)
	}
	if got.AttachmentKey != presign.Key {
		t.Fatalf("附件没落上：%q", got.AttachmentKey)
	}
	if got.AttachmentName != "2026-08 statement.pdf" {
		t.Fatalf("显示名没从 key 里还原出来：%q", got.AttachmentName)
	}
	if got.AttachmentURL == "" {
		t.Fatal("读的时候应该现签一个下载地址")
	}
	// 现签的地址不能落库——存下来就是个必然过期的链接。
	var stored string
	if err := pool.QueryRow(ctx,
		`SELECT attachment_key FROM bank_transactions WHERE tenant_id=$1 AND id=$2`,
		tenantID, row.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != presign.Key || strings.Contains(stored, "http") {
		t.Fatalf("库里存的应该是 key 不是 URL：%q", stored)
	}

	// 附件和「处理完没有」无关：传了纸不代表这笔钱有人认领了。
	if got.ClaimedAmount != "0.00" && got.ClaimedAmount != "0" {
		t.Fatalf("传附件不该动认领量，实际 %q", got.ClaimedAmount)
	}

	// 重传替换：行先指向新的，旧对象随后被收走。
	second, err := svc.PresignBankTransactionFile(ctx, tenantID, row.ID, "statement-v2.pdf", op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AttachBankTransactionFile(ctx, tenantID, row.ID, second.Key, op); err != nil {
		t.Fatal(err)
	}
	if len(files.removed) != 1 || files.removed[0] != presign.Key {
		t.Fatalf("旧对象应该被收走一次，实际 %v", files.removed)
	}

	// 列表里也要看得见附件，否则财务得逐行点开才知道哪些传了纸。
	list, _, err := svc.ListBankTransactions(ctx, tenantID, BankTransactionFilter{}, 1, 20, op)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, v := range list {
		if v.ID == row.ID {
			found = v.AttachmentURL != "" && v.AttachmentName == "statement-v2.pdf"
		}
	}
	if !found {
		t.Fatal("列表里没带出附件的下载地址和文件名")
	}
}

// 没配文件存储时给一句人话，不要 panic——本地开发经常不起 MinIO。
func TestBankAttachmentWithoutStorageFailsCleanly(t *testing.T) {
	svc := New(nil, Deps{})
	if _, err := svc.PresignBankTransactionFile(context.Background(), 1, 1, "a.pdf", Operator{}); err == nil ||
		!strings.Contains(err.Error(), "BANK_FILES_UNAVAILABLE") {
		t.Fatalf("要 BANK_FILES_UNAVAILABLE，实际 %v", err)
	}
	if _, err := svc.AttachBankTransactionFile(context.Background(), 1, 1, "k", Operator{}); err == nil ||
		!strings.Contains(err.Error(), "BANK_FILES_UNAVAILABLE") {
		t.Fatalf("要 BANK_FILES_UNAVAILABLE，实际 %v", err)
	}
}
