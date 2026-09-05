package app

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 一台只会拒绝 MOVE 的假邮件服务器，另外记着每个文件夹里还有哪些 UID。
type refusingHost struct {
	Mailbox
	present map[string]map[uint32]bool // folder → uid → 还在
	moves   int
}

func (h *refusingHost) TrashFolder(context.Context, MailAccount) (string, error) {
	return "已删除", nil
}
func (h *refusingHost) JunkFolder(context.Context, MailAccount) (string, error) {
	return "垃圾邮件", nil
}
func (h *refusingHost) SentFolder(context.Context, MailAccount) (string, error) {
	return "已发送", nil
}
func (h *refusingHost) ArchiveFolder(context.Context, MailAccount) (string, error) { return "", nil }
func (h *refusingHost) MoveMessages(context.Context, MailAccount, string, []uint32, string) error {
	h.moves++
	return errors.New("UID MOVE can't move those messages or to that name")
}
func (h *refusingHost) FetchFlags(_ context.Context, _ MailAccount, folder string, uids []uint32) (map[uint32]MessageFlags, error) {
	out := map[uint32]MessageFlags{}
	for _, u := range uids {
		if h.present[folder][u] {
			out[u] = MessageFlags{}
		}
	}
	return out, nil
}

// 生产上真发生的：员工在 Foxmail 里已经删了，263 上 UID 早没了；我们拿着那个
// 编号 MOVE，263 说 can't move those messages，我们重试到 1265 次。期间
// CountPendingFlagOps > 0，读状态对账被整个封住。
func TestAMoveForAMessageAlreadyGoneFromTheHostIsRetired(t *testing.T) {
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	const me = int64(8001)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_flag_ops WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	res, err := svc.VerifyMailSecret(ctx, tenantID, me, BindRequest{
		Email: "me@263.net", Provider: "p263", Secret: "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	host := &refusingHost{present: map[string]map[uint32]bool{
		"INBOX": {501: true}, // 501 还在，502 已经被别的客户端删掉了
	}}
	svc.UseMailbox(host)

	enqueue := func(uid int64) {
		t.Helper()
		if err := svc.q.EnqueueFlagOp(ctx, store.EnqueueFlagOpParams{
			TenantID: tenantID, AccountID: res.AccountID, EmployeeID: me,
			Folder: "INBOX", ImapUid: uid, Flag: flagTrash, Op: opAdd, MessageID: "m@x",
		}); err != nil {
			t.Fatal(err)
		}
	}
	enqueue(501)
	enqueue(502)

	svc.publishFlagOps(ctx, SyncConfig{TenantID: tenantID})

	var left []int64
	rows, _ := pool.Query(ctx, "SELECT imap_uid FROM mail_flag_ops WHERE tenant_id=$1 ORDER BY imap_uid", tenantID)
	for rows.Next() {
		var u int64
		_ = rows.Scan(&u)
		left = append(left, u)
	}
	rows.Close()
	// 502 在服务器上已经不在了：目的达到（信不在收件箱里），操作作废。
	// 501 还在、MOVE 又被拒：留在队列里按退避重试，这是对的。
	if len(left) != 1 || left[0] != 501 {
		t.Fatalf("队列里应该只剩 501，实际 %v", left)
	}
	if host.moves != 2 {
		t.Errorf("两条各试一次 MOVE，实际 %d 次", host.moves)
	}
}

// 重试要有上限。一条永远失败的操作不能靠退避「变慢」来解决——它每一轮仍然
// 占一次连接，而且只要它在，这个账号的读状态对账就一直不跑。
func TestAMoveThatKeepsFailingIsGivenUpAfterTheCap(t *testing.T) {
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	const me = int64(8002)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_flag_ops WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	box, _ := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	res, err := svc.VerifyMailSecret(ctx, tenantID, me, BindRequest{
		Email: "me@263.net", Provider: "p263", Secret: "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.UseMailbox(&refusingHost{present: map[string]map[uint32]bool{"INBOX": {7: true}}})
	if err := svc.q.EnqueueFlagOp(ctx, store.EnqueueFlagOpParams{
		TenantID: tenantID, AccountID: res.AccountID, EmployeeID: me,
		Folder: "INBOX", ImapUid: 7, Flag: flagTrash, Op: opAdd, MessageID: "m@x",
	}); err != nil {
		t.Fatal(err)
	}
	// 直接把它推到上限前一步，再跑一轮。
	if _, err := pool.Exec(ctx, "UPDATE mail_flag_ops SET attempts=$2, next_try_at=now() WHERE tenant_id=$1",
		tenantID, maxFlagOpAttempts-1); err != nil {
		t.Fatal(err)
	}
	svc.publishFlagOps(ctx, SyncConfig{TenantID: tenantID})

	var n int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM mail_flag_ops WHERE tenant_id=$1", tenantID).Scan(&n)
	if n != 0 {
		t.Fatalf("到上限还失败的操作应该被放弃，队列里还剩 %d 条", n)
	}
}
