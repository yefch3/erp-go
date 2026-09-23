package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 263 那种服务器：回收站里什么都读不到，按 Message-ID 搜一律拒绝。
type refusedSearchHost struct {
	Mailbox
	searches int
	purged   []uint32
}

func (h *refusedSearchHost) TrashFolder(context.Context, MailAccount) (string, error) {
	return "已删除", nil
}
func (h *refusedSearchHost) ArchiveFolder(context.Context, MailAccount) (string, error) {
	return "", nil
}
func (h *refusedSearchHost) JunkFolder(context.Context, MailAccount) (string, error) {
	return "垃圾邮件", nil
}
func (h *refusedSearchHost) SearchFlagged(context.Context, MailAccount, string) ([]uint32, error) {
	return nil, nil
}
func (h *refusedSearchHost) FetchFlags(context.Context, MailAccount, string, []uint32) (map[uint32]MessageFlags, error) {
	return map[uint32]MessageFlags{}, nil // 收件箱里已经没有这封了
}
func (h *refusedSearchHost) RecentMessageIDs(context.Context, MailAccount, string, uint32) (map[string]bool, error) {
	return map[string]bool{}, nil // 回收站最近那一截里也没有
}
func (h *refusedSearchHost) FindUIDByMessageID(context.Context, MailAccount, string, string) (uint32, bool, error) {
	h.searches++
	return 0, false, ErrMessageIDSearchRefused
}
func (h *refusedSearchHost) PurgeMessages(_ context.Context, _ MailAccount, _ string, uids []uint32) error {
	h.purged = append(h.purged, uids...)
	return nil
}

// 已经在我们回收站里的信，服务器那边又找不到了——这是对账里唯一会真删东西
// 的一支，所以它要一个确切的「不在」才动手。服务器拒绝回答不是「不在」：
// 信必须原样留着（回收站 30 天的清扫会照常收走它），而且这件事不再逐封写
// 警告——2026-09-23 生产上一天 54,081 行，把真出事的淹了。
func TestARefusedSearchNeverPurgesAndDoesNotWarnPerMail(t *testing.T) {
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
	const me = int64(9101)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()

	var logs bytes.Buffer
	box, _ := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(&logs, nil)))
	res, err := svc.VerifyMailSecret(ctx, tenantID, me, BindRequest{
		Email: "me@263.net", Provider: "p263", Secret: "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	host := &refusedSearchHost{}
	svc.UseMailbox(host)

	// 用户在 ERP 里删的：deleted_at 有值，服务器上的位置没记下来（COPYUID
	// 那个修法之前删的信都是这样），所以只能按 Message-ID 去问。
	var mailID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, to_email, subject, received_at, deleted_at)
		VALUES ($1, $2, $3, 'binned@mid', 'binned-thr', 'INBOX', 5, 'c@x', 'me@263.net', '已删', now(), now() - interval '1 day')
		RETURNING id`, tenantID, res.AccountID, me).Scan(&mailID); err != nil {
		t.Fatal(err)
	}
	acct, err := svc.ForAccount(ctx, tenantID, res.AccountID)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ { // 三轮对账
		if err := svc.ReconcileFlags(ctx, tenantID, acct, "INBOX", "INBOX", ""); err != nil {
			t.Fatal(err)
		}
	}

	if host.searches == 0 {
		t.Fatal("测试没走到「确认服务器那边删没删」那一支")
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM email_inbound WHERE id=$1`, mailID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(host.purged) != 0 {
		t.Fatalf("服务器拒绝回答不等于「不在了」，信不能删：行=%d 服务器上删了=%v", n, host.purged)
	}
	if strings.Contains(logs.String(), "could not confirm a host purge") {
		t.Fatalf("被拒绝不该逐封写警告：\n%s", logs.String())
	}
}
