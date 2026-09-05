package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 一台只会拒绝 MOVE 的假邮件服务器，另外记着每个文件夹里还有哪些 UID。
type refusingHost struct {
	Mailbox
	present map[string]map[uint32]bool // folder → uid → 还在
	moves   int
	// validity 是每个文件夹此刻的 UIDVALIDITY；0 表示"服务器答不出来"。
	validity map[string]uint32
	purges   int
	// searchBroken 模拟 263：HEADER Message-Id 的 SEARCH 直接被拒。
	searchBroken bool
}

func (h *refusingHost) FolderStatus(_ context.Context, _ MailAccount, folder string) (FolderStatus, error) {
	return FolderStatus{UIDValidity: h.validity[folder]}, nil
}
func (h *refusingHost) FindUIDsByMessageIDs(_ context.Context, _ MailAccount, _ string, ids []string) (map[string]uint32, error) {
	if h.searchBroken {
		return nil, errors.New("UID SEARCH search error: can't search that criteria")
	}
	out := map[string]uint32{}
	for i, id := range ids {
		out[id] = uint32(1000 + i) // 都"找得到"，好让批量清理那条路被走到
	}
	return out, nil
}
func (h *refusingHost) PurgeMessages(context.Context, MailAccount, string, []uint32) error {
	h.purges++
	return errors.New("EXPUNGE failed: mailbox is read-only")
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
	host := &refusingHost{
		present:  map[string]map[uint32]bool{"INBOX": {501: true}}, // 501 还在，502 已经被别的客户端删掉了
		validity: map[string]uint32{"INBOX": 7},
	}
	svc.UseMailbox(host)
	// 我们记的世代和服务器一致：这时"按 UID 查不到"才真的意味着信不在了。
	syncState(t, pool, tenantID, res.AccountID, "INBOX", 7)

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

// syncState 写一行 mail_sync_state，只填判断要用的世代。
func syncState(t *testing.T, pool *pgxpool.Pool, tenantID, accountID int64, folder string, validity uint32) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO mail_sync_state
		(tenant_id, account_id, folder, uid_validity, last_uid, low_uid)
		VALUES ($1, $2, $3, $4, 0, 0)
		ON CONFLICT (tenant_id, account_id, folder) DO UPDATE SET uid_validity = excluded.uid_validity`,
		tenantID, accountID, folder, int64(validity)); err != nil {
		t.Fatal(err)
	}
}

// 文件夹换代之后我们手里的 UID 全部作废：按 UID 查每一封都"不在"，但那不是
// 信没了，是编号没意义了。这时**不能**把操作当成已完成作废——静默作废是所有
// 结果里最坏的一种。让它按退避走到上限、留一条告警。
func TestAMoveIsNotRetiredWhenTheFolderChangedGeneration(t *testing.T) {
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
	const me = int64(8003)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_flag_ops WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_sync_state WHERE tenant_id=$1", tenantID)
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
	// 服务器上 INBOX 已经是第 8 代，UID 9 在新世代里不存在；我们记的还是第 7 代。
	svc.UseMailbox(&refusingHost{
		present:  map[string]map[uint32]bool{"INBOX": {}},
		validity: map[string]uint32{"INBOX": 8},
	})
	syncState(t, pool, tenantID, res.AccountID, "INBOX", 7)
	if err := svc.q.EnqueueFlagOp(ctx, store.EnqueueFlagOpParams{
		TenantID: tenantID, AccountID: res.AccountID, EmployeeID: me,
		Folder: "INBOX", ImapUid: 9, Flag: flagTrash, Op: opAdd, MessageID: "m@x",
	}); err != nil {
		t.Fatal(err)
	}

	svc.publishFlagOps(ctx, SyncConfig{TenantID: tenantID})

	var n int
	var attempts int32
	_ = pool.QueryRow(ctx, "SELECT count(*), coalesce(max(attempts),0) FROM mail_flag_ops WHERE tenant_id=$1", tenantID).Scan(&n, &attempts)
	if n != 1 {
		t.Fatalf("换代之后按 UID 查不到不等于信没了，操作不该被作废；队列里剩 %d 条", n)
	}
	if attempts != 1 {
		t.Errorf("应该记为失败一次、等待重试，attempts=%d", attempts)
	}
}

// 批量清理（PURGE）那条路也要有上限。它是审查里指出的漏网之鱼：单条 MOVE
// 走了带上限的版本，批量 EXPUNGE 失败却还是老样子——一次总失败的批量清理
// 会把整批操作永远留在队列里，顺带封死这个账号的读状态对账。
func TestAFailingBulkPurgeIsAlsoGivenUpAtTheCap(t *testing.T) {
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
	const me = int64(8004)
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
	host := &refusingHost{present: map[string]map[uint32]bool{}, validity: map[string]uint32{}}
	svc.UseMailbox(host)
	for _, uid := range []int64{31, 32} {
		if err := svc.q.EnqueueFlagOp(ctx, store.EnqueueFlagOpParams{
			TenantID: tenantID, AccountID: res.AccountID, EmployeeID: me,
			Folder: "INBOX", ImapUid: uid, Flag: flagPurge, Op: opAdd, MessageID: fmt.Sprintf("m%d@x", uid),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE mail_flag_ops SET attempts=$2, next_try_at=now() WHERE tenant_id=$1",
		tenantID, maxFlagOpAttempts-1); err != nil {
		t.Fatal(err)
	}

	svc.publishFlagOps(ctx, SyncConfig{TenantID: tenantID})

	if host.purges == 0 {
		t.Fatal("批量清理那条路根本没被走到，这条测试没测到目标")
	}
	var n int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM mail_flag_ops WHERE tenant_id=$1", tenantID).Scan(&n)
	if n != 0 {
		t.Fatalf("到上限还失败的批量清理应该被放弃，队列里还剩 %d 条", n)
	}
}

// 263 不认按 Message-Id 的 SEARCH。走到这一支的 PURGE 永远到不了 MOVE，也就
// 碰不到 MOVE 那边的上限——生产上账号 13 的两条就这样重试了一千多次。这一支
// 同样要有上限。
func TestAPurgeWhoseLookupTheHostRejectsIsAlsoGivenUp(t *testing.T) {
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
	const me = int64(8005)
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
	svc.UseMailbox(&refusingHost{present: map[string]map[uint32]bool{}, validity: map[string]uint32{}, searchBroken: true})
	if err := svc.q.EnqueueFlagOp(ctx, store.EnqueueFlagOpParams{
		TenantID: tenantID, AccountID: res.AccountID, EmployeeID: me,
		Folder: "INBOX", ImapUid: 1, Flag: flagPurge, Op: opAdd, MessageID: "gone@x",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE mail_flag_ops SET attempts=$2, next_try_at=now() WHERE tenant_id=$1",
		tenantID, maxFlagOpAttempts-1); err != nil {
		t.Fatal(err)
	}

	svc.publishFlagOps(ctx, SyncConfig{TenantID: tenantID})

	var n int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM mail_flag_ops WHERE tenant_id=$1", tenantID).Scan(&n)
	if n != 0 {
		t.Fatalf("SEARCH 被拒、到上限的 PURGE 应该被放弃，队列里还剩 %d 条", n)
	}
}
