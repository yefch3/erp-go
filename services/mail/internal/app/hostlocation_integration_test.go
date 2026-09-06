package app

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 会给 COPYUID 的假服务器：每次挪都把 UID 加 1000，并记下所有调用。
type locatingHost struct {
	Mailbox
	moves    []string // "from→to:uid"
	searches int      // 按 Message-ID 搜了几次（263 上这一步会失败）
	purged   []uint32
}

func (h *locatingHost) TrashFolder(context.Context, MailAccount) (string, error) {
	return "已删除", nil
}
func (h *locatingHost) JunkFolder(context.Context, MailAccount) (string, error) {
	return "垃圾邮件", nil
}
func (h *locatingHost) SentFolder(context.Context, MailAccount) (string, error) {
	return "已发送", nil
}
func (h *locatingHost) ArchiveFolder(context.Context, MailAccount) (string, error) { return "", nil }
func (h *locatingHost) FolderStatus(context.Context, MailAccount, string) (FolderStatus, error) {
	return FolderStatus{UIDValidity: 7}, nil
}
func (h *locatingHost) MoveMessages(_ context.Context, _ MailAccount, from string, uids []uint32, to string) (map[uint32]uint32, error) {
	out := map[uint32]uint32{}
	for _, u := range uids {
		h.moves = append(h.moves, from+"→"+to+":"+strconv.FormatUint(uint64(u), 10))
		out[u] = u + 1000
	}
	return out, nil
}
func (h *locatingHost) FindUIDsByMessageIDs(_ context.Context, _ MailAccount, _ string, ids []string) (map[string]uint32, error) {
	h.searches++
	return map[string]uint32{}, nil
}
func (h *locatingHost) FindUIDByMessageID(context.Context, MailAccount, string, string) (uint32, bool, error) {
	h.searches++
	return 0, false, nil
}
func (h *locatingHost) PurgeMessages(_ context.Context, _ MailAccount, _ string, uids []uint32) error {
	h.purged = append(h.purged, uids...)
	return nil
}
func (h *locatingHost) FetchFlags(_ context.Context, _ MailAccount, _ string, uids []uint32) (map[uint32]MessageFlags, error) {
	out := map[uint32]MessageFlags{}
	for _, u := range uids {
		out[u] = MessageFlags{}
	}
	return out, nil
}

// 263 不认 Message-ID 搜索，所以彻底删除和恢复在 263 上一直是坏的。修法：
// 挪进回收站时接住 COPYUID 记下新 UID，之后一律按 UID 操作，一次搜索都不发。
func TestDeleteRecordsWhereTheMailWentAndPurgeAndRestoreUseIt(t *testing.T) {
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
	const me = int64(9001)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_flag_ops WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
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
	host := &locatingHost{}
	svc.UseMailbox(host)

	var mailID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, to_email, subject, received_at)
		VALUES ($1, $2, $3, 'loc@mid', 'loc-thr', 'INBOX', 5, 'c@x', 'me@263.net', '位置', now())
		RETURNING id`, tenantID, res.AccountID, me).Scan(&mailID); err != nil {
		t.Fatal(err)
	}
	enqueue := func(flag, op string) {
		t.Helper()
		if err := svc.q.EnqueueFlagOp(ctx, store.EnqueueFlagOpParams{
			TenantID: tenantID, AccountID: res.AccountID, EmployeeID: me,
			Folder: "INBOX", ImapUid: 5, Flag: flag, Op: op, MessageID: "loc@mid",
		}); err != nil {
			t.Fatal(err)
		}
	}
	hostLoc := func() (string, int64) {
		t.Helper()
		var f string
		var u int64
		if err := pool.QueryRow(ctx, `SELECT host_folder, host_uid FROM email_inbound WHERE id=$1`, mailID).Scan(&f, &u); err != nil {
			t.Fatal(err)
		}
		return f, u
	}

	// 1) 删除：挪进回收站，COPYUID 说新号是 1005，记下来。
	enqueue(flagTrash, opAdd)
	svc.publishFlagOps(ctx, SyncConfig{TenantID: tenantID})
	if f, u := hostLoc(); f != "已删除" || u != 1005 {
		t.Fatalf("挪走之后应该记着 (已删除, 1005)，实际 (%q, %d)", f, u)
	}
	if host.searches != 0 {
		t.Fatalf("删除不该搜索，搜了 %d 次", host.searches)
	}

	// 2) 彻底删除：直接拿 1005 去回收站删，一次搜索都不发。
	enqueue(flagPurge, opAdd)
	svc.publishFlagOps(ctx, SyncConfig{TenantID: tenantID})
	if len(host.purged) != 1 || host.purged[0] != 1005 {
		t.Fatalf("彻底删除应该按记下来的 1005 删，实际 %v", host.purged)
	}
	if host.searches != 0 {
		t.Fatalf("有记录就不该搜索（263 上搜索会失败），搜了 %d 次", host.searches)
	}
}

// 恢复：拿记下来的 UID 从回收站挪回来，COPYUID 说回来后是 2005，行的身份改成
// (INBOX, 2005)，记录清空。全程不搜索。
func TestRestoreUsesTheRecordedUIDAndRepointsTheRow(t *testing.T) {
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
	const me = int64(9002)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_flag_ops WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
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
	host := &locatingHost{}
	svc.UseMailbox(host)

	// 一封已经删掉、记着在回收站 1005 号的信。
	var mailID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, to_email, subject, received_at, deleted_at, host_folder, host_uid)
		VALUES ($1, $2, $3, 'r@mid', 'r-thr', 'INBOX', 5, 'c@x', 'me@263.net', '恢复', now(), now(), '已删除', 1005)
		RETURNING id`, tenantID, res.AccountID, me).Scan(&mailID); err != nil {
		t.Fatal(err)
	}
	if err := svc.q.EnqueueFlagOp(ctx, store.EnqueueFlagOpParams{
		TenantID: tenantID, AccountID: res.AccountID, EmployeeID: me,
		Folder: "INBOX", ImapUid: 5, Flag: flagTrash, Op: opRemove, MessageID: "r@mid",
	}); err != nil {
		t.Fatal(err)
	}
	svc.publishFlagOps(ctx, SyncConfig{TenantID: tenantID})

	if len(host.moves) != 1 || host.moves[0] != "已删除→INBOX:1005" {
		t.Fatalf("应该拿记下来的 1005 从回收站挪回收件箱，实际 %v", host.moves)
	}
	var folder, hf string
	var uid, hu int64
	if err := pool.QueryRow(ctx, `SELECT folder, imap_uid, host_folder, host_uid FROM email_inbound WHERE id=$1`, mailID).Scan(&folder, &uid, &hf, &hu); err != nil {
		t.Fatal(err)
	}
	if folder != "INBOX" || uid != 2005 {
		t.Errorf("行的身份应该改成 (INBOX, 2005)，实际 (%s, %d)", folder, uid)
	}
	if hf != "" || hu != 0 {
		t.Errorf("回到正位后记录应该清空，实际 (%q, %d)", hf, hu)
	}
	if host.searches != 0 {
		t.Errorf("恢复不该搜索，搜了 %d 次", host.searches)
	}
}

// 老数据没有记录：彻底删除退回按 Message-ID 搜索那条路（能搜的服务器照常）。
func TestWithoutARecordPurgeFallsBackToSearch(t *testing.T) {
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
	const me = int64(9003)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_flag_ops WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
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
	host := &locatingHost{}
	svc.UseMailbox(host)
	if _, err := pool.Exec(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, to_email, subject, received_at, deleted_at)
		VALUES ($1, $2, $3, 'old@mid', 'old-thr', 'INBOX', 5, 'c@x', 'me@263.net', '老信', now(), now())`,
		tenantID, res.AccountID, me); err != nil {
		t.Fatal(err)
	}
	if err := svc.q.EnqueueFlagOp(ctx, store.EnqueueFlagOpParams{
		TenantID: tenantID, AccountID: res.AccountID, EmployeeID: me,
		Folder: "INBOX", ImapUid: 5, Flag: flagPurge, Op: opAdd, MessageID: "old@mid",
	}); err != nil {
		t.Fatal(err)
	}
	svc.publishFlagOps(ctx, SyncConfig{TenantID: tenantID})
	if host.searches == 0 {
		t.Fatal("没有记录时应该退回按 Message-ID 搜索")
	}
}
