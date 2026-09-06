package app

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 一台记账的假服务器：建/改/删/列目录都记下来，挪信按 COPYUID 给新号。
type folderHost struct {
	Mailbox
	folders  []string
	created  []string
	renamed  []string // "old→new"
	deleted  []string
	moves    []string // "from→to:uid"
	calls    int      // MoveMessages 被叫了几次：批量要一次挪一批
	searches int
}

func (h *folderHost) TrashFolder(context.Context, MailAccount) (string, error) {
	return "已删除", nil
}
func (h *folderHost) JunkFolder(context.Context, MailAccount) (string, error) {
	return "垃圾邮件", nil
}
func (h *folderHost) SentFolder(context.Context, MailAccount) (string, error) {
	return "已发送", nil
}
func (h *folderHost) ArchiveFolder(context.Context, MailAccount) (string, error) { return "", nil }
func (h *folderHost) ListFolders(context.Context, MailAccount) ([]string, error) {
	return append([]string{"INBOX", "已发送", "垃圾邮件", "已删除"}, h.folders...), nil
}
func (h *folderHost) CreateFolder(_ context.Context, _ MailAccount, name string) error {
	h.created = append(h.created, name)
	h.folders = append(h.folders, name)
	return nil
}
func (h *folderHost) RenameFolder(_ context.Context, _ MailAccount, oldName, newName string) error {
	h.renamed = append(h.renamed, oldName+"→"+newName)
	return nil
}
func (h *folderHost) DeleteFolder(_ context.Context, _ MailAccount, name string) error {
	h.deleted = append(h.deleted, name)
	return nil
}
func (h *folderHost) MoveMessages(_ context.Context, _ MailAccount, from string, uids []uint32, to string) (map[uint32]uint32, error) {
	h.calls++
	out := map[uint32]uint32{}
	for _, u := range uids {
		h.moves = append(h.moves, from+"→"+to)
		out[u] = u + 1000
	}
	return out, nil
}
func (h *folderHost) FindUIDByMessageID(context.Context, MailAccount, string, string) (uint32, bool, error) {
	h.searches++
	return 0, false, errors.New("263 不认这种搜索")
}

type folderFixture struct {
	pool     *pgxpool.Pool
	svc      *Service
	host     *folderHost
	tenantID int64
	account  int64
	me       int64
}

func newFolderFixture(t *testing.T, me int64) *folderFixture {
	t.Helper()
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	t.Cleanup(func() {
		for _, tbl := range []string{"mail_flag_ops", "mail_folders", "email_inbound", "mail_sync_state", "mail_binding_log", "mail_accounts"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+tbl+" WHERE tenant_id=$1", tenantID)
		}
		pool.Close()
	})
	box, _ := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	res, err := svc.VerifyMailSecret(ctx, tenantID, me, BindRequest{
		Email: "me@263.net", Provider: "p263", Secret: "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	host := &folderHost{}
	svc.UseMailbox(host)
	return &folderFixture{pool: pool, svc: svc, host: host, tenantID: tenantID, account: res.AccountID, me: me}
}

func (f *folderFixture) insertMail(t *testing.T, folder string, uid int64, subject string) int64 {
	t.Helper()
	return f.insertThreadMail(t, folder, uid, subject, subject+"-thr")
}

// insertThreadMail 插一封指定会话的信：同一个 thread 的几封就是一条会话。
func (f *folderFixture) insertThreadMail(t *testing.T, folder string, uid int64, subject, thread string) int64 {
	t.Helper()
	var id int64
	if err := f.pool.QueryRow(context.Background(), `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, to_email, subject, received_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'c@x', 'me@263.net', $8, now()) RETURNING id`,
		f.tenantID, f.account, f.me, subject+"@mid", thread, folder, uid, subject).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *folderFixture) viewsOf(t *testing.T, mailID int64) []string {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT view FROM mail_thread_view
		WHERE tenant_id=$1 AND last_id=$2 ORDER BY view`, f.tenantID, mailID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		out = append(out, v)
	}
	return out
}

// 建：先在服务器上建，成了再登记。名字不合规的根本不去碰服务器。
func TestCreateFolderMakesItOnTheHostThenRegistersIt(t *testing.T) {
	f := newFolderFixture(t, 9101)
	ctx := context.Background()
	got, err := f.svc.CreateMailFolder(ctx, f.tenantID, f.me, f.account, "  供应商  ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "供应商" || got.HostName != "供应商" || got.ViewKey() != "F:供应商" {
		t.Errorf("登记的名字不对：%+v", got)
	}
	if len(f.host.created) != 1 || f.host.created[0] != "供应商" {
		t.Errorf("服务器上应该建了一个「供应商」，实际 %v", f.host.created)
	}
	// 重名
	if _, err := f.svc.CreateMailFolder(ctx, f.tenantID, f.me, f.account, "供应商"); err == nil || !strings.Contains(err.Error(), "MAIL_FOLDER_EXISTS") {
		t.Errorf("重名应该被拒：%v", err)
	}
	// 不合规的名字：不该碰服务器
	before := len(f.host.created)
	for _, bad := range []string{"", "   ", "客户/ACME", "INBOX", "inbox", "Trash", "[Gmail]/x", "a*b", `q"q`, strings.Repeat("长", 121)} {
		if _, err := f.svc.CreateMailFolder(ctx, f.tenantID, f.me, f.account, bad); err == nil {
			t.Errorf("%q 不该允许", bad)
		}
	}
	if len(f.host.created) != before {
		t.Errorf("不合规的名字碰了服务器：%v", f.host.created)
	}
	// 别人的信箱：一律「不存在」
	if _, err := f.svc.CreateMailFolder(ctx, f.tenantID, f.me+1, f.account, "别人的"); err == nil {
		t.Error("不该允许在别人的信箱里建文件夹")
	}
}

// 列：服务器上有、我们没登记的普通文件夹自动登记进来（Foxmail 里建的）；
// 系统文件夹不算。
func TestListImportsFoldersMadeElsewhere(t *testing.T) {
	f := newFolderFixture(t, 9102)
	f.host.folders = []string{"客户跟进", "[Gmail]/All Mail", "Drafts"}
	got, err := f.svc.ListMailFolders(context.Background(), f.tenantID, f.me, f.account)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "客户跟进" {
		t.Fatalf("应该只导入「客户跟进」，实际 %+v", got)
	}
}

// 改名：服务器 RENAME，登记改名，行里的 folder 跟着改，视图跟着变。
func TestRenameFolderRelabelsItsMail(t *testing.T) {
	f := newFolderFixture(t, 9103)
	ctx := context.Background()
	fd, err := f.svc.CreateMailFolder(ctx, f.tenantID, f.me, f.account, "旧名")
	if err != nil {
		t.Fatal(err)
	}
	mail := f.insertMail(t, "旧名", 11, "在旧名里")
	if v := f.viewsOf(t, mail); len(v) != 1 || v[0] != "F:旧名" {
		t.Fatalf("改名前视图应该是 F:旧名，实际 %v", v)
	}
	if _, err := f.svc.RenameMailFolder(ctx, f.tenantID, f.me, fd.ID, "新名"); err != nil {
		t.Fatal(err)
	}
	if len(f.host.renamed) != 1 || f.host.renamed[0] != "旧名→新名" {
		t.Errorf("服务器上应该改名：%v", f.host.renamed)
	}
	if v := f.viewsOf(t, mail); len(v) != 1 || v[0] != "F:新名" {
		t.Errorf("改名后视图应该是 F:新名，实际 %v", v)
	}
}

// 删：里面还有信就不删，服务器也不碰。空了才删。
func TestDeleteFolderRefusesWhileItStillHoldsMail(t *testing.T) {
	f := newFolderFixture(t, 9104)
	ctx := context.Background()
	fd, err := f.svc.CreateMailFolder(ctx, f.tenantID, f.me, f.account, "待清")
	if err != nil {
		t.Fatal(err)
	}
	mail := f.insertMail(t, "待清", 21, "还在里面")
	err = f.svc.DeleteMailFolder(ctx, f.tenantID, f.me, fd.ID)
	if err == nil || !strings.Contains(err.Error(), "MAIL_FOLDER_NOT_EMPTY") {
		t.Fatalf("有信时应该拒绝：%v", err)
	}
	if len(f.host.deleted) != 0 {
		t.Fatalf("拒绝的时候不该碰服务器：%v", f.host.deleted)
	}
	// 把信挪回收件箱，再删就行
	if err := f.svc.MoveInbound(ctx, f.tenantID, f.me, mail, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteMailFolder(ctx, f.tenantID, f.me, fd.ID); err != nil {
		t.Fatalf("空了应该能删：%v", err)
	}
	if len(f.host.deleted) != 1 || f.host.deleted[0] != "待清" {
		t.Errorf("服务器上应该删掉「待清」：%v", f.host.deleted)
	}
	if _, err := f.svc.ListMailFolders(ctx, f.tenantID, f.me, f.account); err != nil {
		t.Fatal(err)
	}
}

// 挪：真的 MOVE，新 UID 从 COPYUID 拿，行的身份当场改，视图跟着变，零搜索。
// 挪回收件箱同理，且归档标记去掉。
func TestMoveInboundRepointsTheRowAndTheView(t *testing.T) {
	f := newFolderFixture(t, 9105)
	ctx := context.Background()
	fd, err := f.svc.CreateMailFolder(ctx, f.tenantID, f.me, f.account, "项目A")
	if err != nil {
		t.Fatal(err)
	}
	mail := f.insertMail(t, "INBOX", 31, "要归类的")
	if _, err := f.pool.Exec(ctx, `UPDATE email_inbound SET archived_at=now() WHERE id=$1`, mail); err != nil {
		t.Fatal(err)
	}
	if v := f.viewsOf(t, mail); len(v) != 1 || v[0] != "ARCHIVE" {
		t.Fatalf("起点应该在归档：%v", v)
	}

	if err := f.svc.MoveInbound(ctx, f.tenantID, f.me, mail, fd.ID); err != nil {
		t.Fatal(err)
	}
	var folder string
	var uid int64
	if err := f.pool.QueryRow(ctx, `SELECT folder, imap_uid FROM email_inbound WHERE id=$1`, mail).Scan(&folder, &uid); err != nil {
		t.Fatal(err)
	}
	if folder != "项目A" || uid != 1031 {
		t.Errorf("行的身份应该改成 (项目A, 1031)，实际 (%s, %d)", folder, uid)
	}
	if v := f.viewsOf(t, mail); len(v) != 1 || v[0] != "F:项目A" {
		t.Errorf("视图应该是 F:项目A，实际 %v", v)
	}
	if len(f.host.moves) != 1 || f.host.moves[0] != "INBOX→项目A" {
		t.Errorf("服务器上应该从 INBOX 挪到 项目A：%v", f.host.moves)
	}
	if f.host.searches != 0 {
		t.Errorf("有 COPYUID 就不该搜索，搜了 %d 次", f.host.searches)
	}

	// 挪回收件箱：归档标记去掉，回到 INBOX 视图。
	if err := f.svc.MoveInbound(ctx, f.tenantID, f.me, mail, 0); err != nil {
		t.Fatal(err)
	}
	if v := f.viewsOf(t, mail); len(v) != 1 || v[0] != "INBOX" {
		t.Errorf("挪回来应该在收件箱，实际 %v", v)
	}

	// 别人的信 / 已发送的信
	if err := f.svc.MoveInbound(ctx, f.tenantID, f.me+1, mail, fd.ID); err == nil {
		t.Error("不该允许挪别人的信")
	}
	sent := f.insertMail(t, "SENT", 41, "已发送的")
	if err := f.svc.MoveInbound(ctx, f.tenantID, f.me, sent, fd.ID); err == nil || !strings.Contains(err.Error(), "MAIL_MOVE_SENT") {
		t.Errorf("已发送不该能挪：%v", err)
	}
}

// 「全部已读」只动当前视图。自建文件夹视图下它以前会落到收件箱那档，把真正
// 收件箱的未读全标掉；认不得的视图（前端传错键时是 "undefined"）一封都不该动。
func TestMarkViewReadStaysInsideTheCustomFolder(t *testing.T) {
	f := newFolderFixture(t, 9106)
	ctx := context.Background()
	fd, err := f.svc.CreateMailFolder(ctx, f.tenantID, f.me, f.account, "项目A")
	if err != nil {
		t.Fatal(err)
	}
	inbox := f.insertMail(t, "INBOX", 51, "留在收件箱的")
	filed := f.insertMail(t, "INBOX", 52, "归到项目A的")
	if err := f.svc.MoveInbound(ctx, f.tenantID, f.me, filed, fd.ID); err != nil {
		t.Fatal(err)
	}
	isRead := func(id int64) bool {
		var r bool
		if err := f.pool.QueryRow(ctx, `SELECT is_read FROM email_inbound WHERE id=$1`, id).Scan(&r); err != nil {
			t.Fatal(err)
		}
		return r
	}

	if _, err := f.svc.MarkViewRead(ctx, f.tenantID, f.me, "undefined"); err == nil || !strings.Contains(err.Error(), "MAIL_VIEW_UNKNOWN") {
		t.Errorf("认不得的视图应该拒绝：%v", err)
	}
	if isRead(inbox) || isRead(filed) {
		t.Fatal("认不得的视图不该动任何一封")
	}

	n, err := f.svc.MarkViewRead(ctx, f.tenantID, f.me, "F:项目A")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || !isRead(filed) || isRead(inbox) {
		t.Errorf("F:项目A 应该只标文件夹里那一封：n=%d filed=%v inbox=%v", n, isRead(filed), isRead(inbox))
	}

	// 关键词搜索走的是同一套视图过滤：在文件夹里搜只该看到文件夹里的。
	page, err := f.svc.ListInbound(ctx, f.tenantID, f.me, f.account, "的", "F:项目A", "", 20, ListSort{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Mails) != 1 || page.Mails[0].Subject != "归到项目A的" {
		t.Errorf("文件夹里搜索应该只有那一封，实际 %d 封", len(page.Mails))
	}
}

// 一键移动：列表一行是一条会话，整条会话一起挪；同一来源文件夹的信一次 MOVE
// 挪完，不是一封一次登录。已发送的、不存在的算失败，不拖累其他封。
func TestMoveInboundBatchMovesWholeThreadsInOneHostCall(t *testing.T) {
	f := newFolderFixture(t, 9107)
	ctx := context.Background()
	fd, err := f.svc.CreateMailFolder(ctx, f.tenantID, f.me, f.account, "项目B")
	if err != nil {
		t.Fatal(err)
	}
	a1 := f.insertThreadMail(t, "INBOX", 61, "会话A 第一封", "thr-A")
	a2 := f.insertThreadMail(t, "INBOX", 62, "会话A 第二封", "thr-A")
	b := f.insertThreadMail(t, "INBOX", 63, "单独一封", "thr-B")
	sent := f.insertMail(t, "SENT", 64, "已发送的")
	stay := f.insertMail(t, "INBOX", 65, "没勾的")
	where := func(id int64) (string, int64) {
		var folder string
		var uid int64
		if err := f.pool.QueryRow(ctx, `SELECT folder, imap_uid FROM email_inbound WHERE id=$1`, id).Scan(&folder, &uid); err != nil {
			t.Fatal(err)
		}
		return folder, uid
	}

	// 勾了会话A 的最新一封和单独一封，外加一封已发送、一个不存在的 id。
	f.host.calls = 0
	moved, failed, err := f.svc.MoveInboundBatch(ctx, f.tenantID, f.me, []int64{a2, b, sent, 424242}, fd.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if moved != 3 {
		t.Errorf("应该挪 3 封（会话A 两封 + 单独一封），实际 %d", moved)
	}
	if len(failed) != 2 {
		t.Errorf("已发送的和不存在的应该算失败，失败清单 %v", failed)
	}
	if f.host.calls != 1 {
		t.Errorf("同一来源文件夹应该一次 MOVE 挪完，服务器被叫了 %d 次", f.host.calls)
	}
	if f.host.searches != 0 {
		t.Errorf("有 COPYUID 就不该搜索，搜了 %d 次", f.host.searches)
	}
	for _, id := range []int64{a1, a2, b} {
		if folder, uid := where(id); folder != "项目B" || uid < 1061 || uid > 1063 {
			t.Errorf("信 %d 应该在 (项目B, 106x)，实际 (%s, %d)", id, folder, uid)
		}
	}
	if folder, _ := where(stay); folder != "INBOX" {
		t.Errorf("没勾的那封不该动，实际在 %s", folder)
	}
	if folder, _ := where(sent); folder != "SENT" {
		t.Errorf("已发送的不该动，实际在 %s", folder)
	}
	if v := f.viewsOf(t, a2); len(v) != 1 || v[0] != "F:项目B" {
		t.Errorf("会话A 的视图应该是 F:项目B，实际 %v", v)
	}

	// 挪回收件箱：勾会话A 的第一封和单独一封，整条会话跟着回来，仍然一次 MOVE。
	f.host.calls = 0
	moved, failed, err = f.svc.MoveInboundBatch(ctx, f.tenantID, f.me, []int64{a1, b}, 0, true)
	if err != nil || moved != 3 || len(failed) != 0 || f.host.calls != 1 {
		t.Errorf("挪回收件箱：moved=%d failed=%v calls=%d err=%v", moved, failed, f.host.calls, err)
	}
	if folder, _ := where(a2); folder != "INBOX" {
		t.Errorf("会话A 的第二封应该跟着回收件箱，实际在 %s", folder)
	}

	// 别人的信挪不了；空清单直接拒绝。
	if _, failed, err := f.svc.MoveInboundBatch(ctx, f.tenantID, f.me+1, []int64{a1}, fd.ID, true); err != nil || len(failed) != 1 {
		t.Errorf("别人的信应该算失败：failed=%v err=%v", failed, err)
	}
	if _, _, err := f.svc.MoveInboundBatch(ctx, f.tenantID, f.me, nil, fd.ID, true); err == nil {
		t.Error("空清单应该拒绝")
	}
}
