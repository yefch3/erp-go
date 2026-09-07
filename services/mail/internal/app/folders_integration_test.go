package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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
	special  []string                // 带 special-use 属性的（服务器自带）
	virtual  []string                // 带 \All / \Flagged 一类属性的（内容是别处的信的映射）
	refuse   error                   // 设了之后 RENAME/DELETE 一律用它拒绝（模拟 263 的 default folder）
	onList   func()                  // LIST 算完结果、还没返回时叫一下：模拟 LIST 进行中别处建了文件夹
	archive  string                  // 服务器的归档文件夹；空 = 没有
	serves   map[string][]RawMessage // 每个文件夹上服务器给的信
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
func (h *folderHost) ArchiveFolder(context.Context, MailAccount) (string, error) {
	return h.archive, nil
}
func (h *folderHost) ListFolders(context.Context, MailAccount) ([]HostFolder, error) {
	var out []HostFolder
	for _, n := range append([]string{"INBOX", "已发送", "垃圾邮件", "已删除"}, h.folders...) {
		out = append(out, HostFolder{Name: n})
	}
	for _, n := range h.special {
		out = append(out, HostFolder{Name: n, Special: true, Role: "SYSTEM"})
	}
	for _, n := range h.virtual {
		out = append(out, HostFolder{Name: n, Special: true, Role: "VIRTUAL"})
	}
	if h.onList != nil {
		h.onList()
	}
	return out, nil
}
func (h *folderHost) CreateFolder(_ context.Context, _ MailAccount, name string) error {
	h.created = append(h.created, name)
	h.folders = append(h.folders, name)
	return nil
}
func (h *folderHost) RenameFolder(_ context.Context, _ MailAccount, oldName, newName string) error {
	if h.refuse != nil {
		return h.refuse
	}
	h.renamed = append(h.renamed, oldName+"→"+newName)
	return nil
}
func (h *folderHost) DeleteFolder(_ context.Context, _ MailAccount, name string) error {
	if h.refuse != nil {
		return h.refuse
	}
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

// 列：服务器 LIST 回来的**全部**文件夹都登记，各带角色；顺序是认得的系统
// 文件夹在前、自建在后。角色按可信度：定位到的已发送/垃圾/已删除 → 属性 →
// [Gmail]/ → 各家默认名（草稿箱是 DRAFTS，其余 SYSTEM）→ 都不是才算自建。
func TestListingRegistersEveryHostFolderWithItsRole(t *testing.T) {
	f := newFolderFixture(t, 9102)
	f.host.folders = []string{"客户跟进", "[Gmail]/All Mail", "Drafts", "草稿箱", "已归档", "病毒文件夹", "Sent Messages"}
	f.host.special = []string{"Archive2"}
	got, err := f.svc.ListMailFolders(context.Background(), f.tenantID, f.me, f.account)
	if err != nil {
		t.Fatal(err)
	}
	roles := map[string]string{}
	for _, g := range got {
		roles[g.Name] = g.Role
	}
	want := map[string]string{
		"INBOX": roleInbox, "已发送": roleSent, "垃圾邮件": roleJunk, "已删除": roleTrash,
		"Drafts": roleDrafts, "草稿箱": roleDrafts,
		"[Gmail]/All Mail": roleVirtual, "已归档": roleSystem, "病毒文件夹": roleSystem, "Sent Messages": roleSystem, "Archive2": roleSystem,
		"客户跟进": roleCustom,
	}
	for name, role := range want {
		if roles[name] != role {
			t.Errorf("%s 的角色应该是 %s，实际 %q", name, role, roles[name])
		}
	}
	if len(got) != len(want) {
		t.Errorf("应该登记 %d 个，实际 %d：%+v", len(want), len(got), got)
	}
	at := func(name string) int {
		for k, g := range got {
			if g.Name == name {
				return k
			}
		}
		return -1
	}
	// 顺序：认得的系统文件夹 → 服务器自带但不认得的 → 自建 → 虚拟的。
	if got[0].Name != "INBOX" || !(at("病毒文件夹") < at("客户跟进")) || !(at("客户跟进") < at("[Gmail]/All Mail")) {
		t.Errorf("顺序不对：%+v", got)
	}
	// 能装信的（自建、服务器自带的真文件夹）才有 F: 视图。
	if got[at("客户跟进")].ViewKey() != "F:客户跟进" || got[at("病毒文件夹")].ViewKey() != "F:病毒文件夹" {
		t.Errorf("自建和系统文件夹都该有 F: 视图：%q / %q", got[at("客户跟进")].ViewKey(), got[at("病毒文件夹")].ViewKey())
	}
	if got[0].ViewKey() != "" || got[at("[Gmail]/All Mail")].ViewKey() != "" {
		t.Errorf("收件箱和虚拟文件夹不该有 F: 视图：%q / %q", got[0].ViewKey(), got[at("[Gmail]/All Mail")].ViewKey())
	}

	// 服务器上没了（Foxmail 里删的）：没信就清掉；有信的留着。
	kept := f.insertMail(t, "客户跟进", 81, "留在里面的")
	_ = kept
	f.host.folders = []string{"[Gmail]/All Mail", "Drafts", "草稿箱", "已归档", "病毒文件夹"}
	f.host.special = nil
	// 直接叫对账那一步：登记表非空时 ListMailFolders 是把对账放到后台去的
	// （见 refreshHostFoldersInBackground），在测试里等它落地只会让这条断言
	// 看运气。清理这件事本身在下面照常验。
	acct, err := f.svc.ForAccount(context.Background(), f.tenantID, f.account)
	if err != nil {
		t.Fatal(err)
	}
	f.svc.syncHostFolders(context.Background(), f.tenantID, f.me, acct)
	got, err = f.svc.ListMailFolders(context.Background(), f.tenantID, f.me, f.account)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, g := range got {
		names[g.Name] = true
	}
	if names["Sent Messages"] || names["Archive2"] {
		t.Errorf("服务器上没了、里面又没信的应该清掉：%+v", got)
	}
	if !names["客户跟进"] {
		t.Error("服务器上没了但里面还有信的要留着，不然信失去文件夹")
	}
}

// 系统文件夹不能改名删除：ERP 自己拦，根本不往服务器发。漏网成 CUSTOM 的，
// 服务器答「默认文件夹」时自动改成 SYSTEM——服务器的拒绝是最后的裁判。
func TestSystemFoldersCannotBeRenamedOrDeleted(t *testing.T) {
	f := newFolderFixture(t, 9109)
	ctx := context.Background()
	f.host.folders = []string{"草稿箱", "临时"}
	got, err := f.svc.ListMailFolders(ctx, f.tenantID, f.me, f.account)
	if err != nil {
		t.Fatal(err)
	}
	var drafts, tmp MailFolder
	for _, g := range got {
		switch g.Name {
		case "草稿箱":
			drafts = g
		case "临时":
			tmp = g
		}
	}
	if drafts.Role != roleDrafts || tmp.Role != roleCustom {
		t.Fatalf("角色不对：草稿箱=%s 临时=%s", drafts.Role, tmp.Role)
	}
	if _, err := f.svc.RenameMailFolder(ctx, f.tenantID, f.me, drafts.ID, "草稿2"); err == nil || !strings.Contains(err.Error(), "MAIL_FOLDER_SYSTEM") {
		t.Errorf("系统文件夹改名应该被拒：%v", err)
	}
	if err := f.svc.DeleteMailFolder(ctx, f.tenantID, f.me, drafts.ID); err == nil || !strings.Contains(err.Error(), "MAIL_FOLDER_SYSTEM") {
		t.Errorf("系统文件夹删除应该被拒：%v", err)
	}
	if len(f.host.renamed) != 0 || len(f.host.deleted) != 0 {
		t.Errorf("被拒的不该碰服务器：renamed=%v deleted=%v", f.host.renamed, f.host.deleted)
	}

	// 漏网的：服务器说它是默认文件夹 → 改成 SYSTEM，下次列表不再是 CUSTOM。
	f.host.refuse = errors.New("RENAME can't rename default folder or Invalid folder name")
	if _, err := f.svc.RenameMailFolder(ctx, f.tenantID, f.me, tmp.ID, "临时2"); err == nil || !strings.Contains(err.Error(), "MAIL_FOLDER_SYSTEM") {
		t.Errorf("服务器答默认文件夹时应该报 MAIL_FOLDER_SYSTEM：%v", err)
	}
	f.host.refuse = nil
	got, _ = f.svc.ListMailFolders(ctx, f.tenantID, f.me, f.account)
	for _, g := range got {
		if g.Name == "临时" && g.Role != roleSystem {
			t.Errorf("服务器拒绝之后角色应该改成 SYSTEM，实际 %s", g.Role)
		}
	}
	// 从此挪信也挪不进去了。
	m := f.insertMail(t, "INBOX", 91, "想挪进去的")
	if err := f.svc.MoveInbound(ctx, f.tenantID, f.me, m, tmp.ID); err == nil || !strings.Contains(err.Error(), "MAIL_FOLDER_SYSTEM") {
		t.Errorf("系统文件夹不该能当挪信目标：%v", err)
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

// 上限卡的是整条会话展开之后的封数，不是勾选的行数；而且卡在碰服务器之前。
func TestMoveInboundBatchCapsTheExpandedCountBeforeTouchingTheHost(t *testing.T) {
	f := newFolderFixture(t, 9108)
	ctx := context.Background()
	fd, err := f.svc.CreateMailFolder(ctx, f.tenantID, f.me, f.account, "项目C")
	if err != nil {
		t.Fatal(err)
	}
	var newest int64
	for i := int64(0); i < 4; i++ {
		newest = f.insertThreadMail(t, "INBOX", 71+i, fmt.Sprintf("长会话 第%d封", i+1), "thr-long")
	}
	was := maxBatchMove
	maxBatchMove = 3
	t.Cleanup(func() { maxBatchMove = was })

	f.host.calls = 0
	_, _, err = f.svc.MoveInboundBatch(ctx, f.tenantID, f.me, []int64{newest}, fd.ID, true)
	if err == nil || !strings.Contains(err.Error(), "MAIL_MOVE_TOO_MANY") {
		t.Fatalf("勾 1 行、展开 4 封、上限 3：应该拒绝，实际 %v", err)
	}
	if f.host.calls != 0 {
		t.Errorf("拒绝之前不该碰服务器，碰了 %d 次", f.host.calls)
	}
	var folder string
	if err := f.pool.QueryRow(ctx, `SELECT folder FROM email_inbound WHERE id=$1`, newest).Scan(&folder); err != nil || folder != "INBOX" {
		t.Errorf("拒绝了就一封都不该动，实际在 %s（err=%v）", folder, err)
	}
	// 不展开会话时只有 1 封，放行。
	if moved, failed, err := f.svc.MoveInboundBatch(ctx, f.tenantID, f.me, []int64{newest}, fd.ID, false); err != nil || moved != 1 || len(failed) != 0 {
		t.Errorf("不展开时 1 封应该放行：moved=%d failed=%v err=%v", moved, failed, err)
	}
}

// LIST 进行中另一个页面刚建的文件夹（服务器上有、库里刚登记、里面还没信）
// 不能被当成"服务器上没了"清掉——否则刚建的文件夹一刷新就消失一次。
func TestAFolderCreatedDuringTheListIsNotSweptAway(t *testing.T) {
	f := newFolderFixture(t, 9110)
	ctx := context.Background()
	f.host.onList = func() {
		// LIST 的结果已经算好（里面没有「新建的」），这时另一个页面建成了它。
		f.host.onList = nil
		f.host.folders = append(f.host.folders, "新建的")
		if _, err := f.pool.Exec(ctx, `INSERT INTO mail_folders (tenant_id, account_id, name, host_name, role, created_by)
			VALUES ($1, $2, '新建的', '新建的', 'CUSTOM', $3)`, f.tenantID, f.account, f.me); err != nil {
			t.Fatal(err)
		}
	}
	got, err := f.svc.ListMailFolders(ctx, f.tenantID, f.me, f.account)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range got {
		if g.Name == "新建的" {
			found = true
		}
	}
	if !found {
		t.Errorf("LIST 进行中建的文件夹被清掉了：%+v", got)
	}
}

func (h *folderHost) Fetch(_ context.Context, _ MailAccount, folder string, sinceUID, _ uint32) (FetchResult, error) {
	res := FetchResult{UIDValidity: 9}
	for _, m := range h.serves[folder] {
		if m.UID > sinceUID {
			res.Messages = append(res.Messages, m)
		}
	}
	return res, nil
}

func (h *folderHost) FetchBelow(context.Context, MailAccount, string, uint32, uint32) (FetchResult, error) {
	return FetchResult{UIDValidity: 9}, nil
}
func (h *folderHost) SearchFlagged(context.Context, MailAccount, string) ([]uint32, error) {
	return nil, nil
}
func (h *folderHost) FetchFlags(_ context.Context, _ MailAccount, folder string, uids []uint32) (map[uint32]MessageFlags, error) {
	have := map[uint32]bool{}
	for _, m := range h.serves[folder] {
		have[m.UID] = true
	}
	out := map[uint32]MessageFlags{}
	for _, u := range uids {
		if have[u] {
			out[u] = MessageFlags{}
		}
	}
	return out, nil
}
func (h *folderHost) RecentMessageIDs(context.Context, MailAccount, string, uint32) (map[string]bool, error) {
	return map[string]bool{}, nil
}

// rawMail 是一封最简单的、能被解析的信。
func rawMail(mid, subject string) []byte {
	return []byte("From: c@x.com\r\nTo: me@263.net\r\nSubject: " + subject +
		"\r\nMessage-ID: <" + mid + ">\r\nDate: Mon, 01 Sep 2026 10:00:00 +0800\r\n\r\n正文\r\n")
}

// 自建文件夹和服务器自带的真文件夹，内容也收进来——员工在 Foxmail 里把信拖进
// 「重要客户」，ERP 里也看得到。这正是 #362 那条 v1 边界。
//
// 不收的三类各有各的理由：归档和回收站收进来会让同一封信多出一行（ERP 的
// 归档/删除是"行留在收件箱加标记、服务器那份挪走"）；草稿箱下一期；虚拟
// 文件夹（Gmail 的标签）收进来会把每封信按标签存好几遍。
func TestCustomAndSystemFoldersGetTheirMailWhileTheRestAreLeftAlone(t *testing.T) {
	f := newFolderFixture(t, 9111)
	ctx := context.Background()
	f.host.archive = "已归档"
	f.host.folders = []string{"重要客户", "病毒文件夹", "已归档", "草稿箱", "[Gmail]/Important"}
	f.host.serves = map[string][]RawMessage{
		"INBOX":             {{UID: 1, Raw: rawMail("in@mid", "收件箱的")}},
		"重要客户":              {{UID: 11, Raw: rawMail("a@mid", "客户报价")}, {UID: 12, Raw: rawMail("b@mid", "客户回复")}},
		"病毒文件夹":             {{UID: 21, Raw: rawMail("c@mid", "可疑附件")}},
		"已归档":               {{UID: 31, Raw: rawMail("d@mid", "归档的")}},
		"草稿箱":               {{UID: 41, Raw: rawMail("e@mid", "写了一半")}},
		"[Gmail]/Important": {{UID: 51, Raw: rawMail("g@mid", "重要标签")}},
	}
	if _, err := f.svc.ListMailFolders(ctx, f.tenantID, f.me, f.account); err != nil {
		t.Fatal(err)
	}

	// 走真正的入口，不是直接调 syncExtraFolders：这样「同步循环里到底有没有
	// 叫它」也被钉住——少了那一行，功能整个不存在，而单独测那个函数照样绿。
	cfg := SyncConfig{TenantID: f.tenantID, BatchSize: 50, HistoryCap: 500}
	if _, err := f.svc.SyncMailbox(ctx, cfg, f.account); err != nil {
		t.Fatal(err)
	}

	count := func(folder string) int {
		t.Helper()
		var n int
		if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM email_inbound
			WHERE tenant_id=$1 AND account_id=$2 AND folder=$3`, f.tenantID, f.account, folder).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count("重要客户"); n != 2 {
		t.Errorf("自建文件夹里的信应该收进来 2 封，实际 %d", n)
	}
	if n := count("病毒文件夹"); n != 1 {
		t.Errorf("服务器自带的真文件夹也该收，实际 %d 封", n)
	}
	for _, folder := range []string{"已归档", "草稿箱", "[Gmail]/Important"} {
		if n := count(folder); n != 0 {
			t.Errorf("%s 不该被同步，却收了 %d 封", folder, n)
		}
	}
	// 收进来的信落在自己的视图里，点左栏那个文件夹就看得到。
	var view string
	if err := f.pool.QueryRow(ctx, `SELECT view FROM mail_thread_view v
		JOIN email_inbound i ON i.id = v.last_id
		WHERE i.tenant_id=$1 AND i.folder='重要客户' LIMIT 1`, f.tenantID).Scan(&view); err != nil {
		t.Fatal(err)
	}
	if view != "F:重要客户" {
		t.Errorf("视图应该是 F:重要客户，实际 %q", view)
	}

	// 再跑一遍：游标记住了，不重复存。
	if _, err := f.svc.SyncMailbox(ctx, cfg, f.account); err != nil {
		t.Fatal(err)
	}
	if n := count("重要客户"); n != 2 {
		t.Errorf("第二遍不该重复存，实际 %d 封", n)
	}
}

// 文件夹名长一点也要能用。视图键是 'F:' 加服务器上的名字，而
// mail_thread_view.view 曾经是 varchar(16)（00034 定的，那时它只可能是 INBOX、
// TRASH 这几个词）。名字超过 14 个字之后：往里挪信整条写入失败（那张表由
// email_inbound 上的触发器维护），同步进来的信被丢掉、只留一行警告。生产上
// 一直没人起过这么长的名字，所以从 #375 上线起就带着这个毛病没露出来。
func TestALongFolderNameStillWorksEndToEnd(t *testing.T) {
	f := newFolderFixture(t, 9112)
	ctx := context.Background()
	const long = "重要客户跟进记录2026年度汇总表" // 17 个字，视图键 19 个字符

	fd, err := f.svc.CreateMailFolder(ctx, f.tenantID, f.me, f.account, long)
	if err != nil {
		t.Fatal(err)
	}
	mail := f.insertMail(t, "INBOX", 71, "要归类的")
	if err := f.svc.MoveInbound(ctx, f.tenantID, f.me, mail, fd.ID); err != nil {
		t.Fatalf("挪进长名字的文件夹失败：%v", err)
	}
	if v := f.viewsOf(t, mail); len(v) != 1 || v[0] != "F:"+long {
		t.Errorf("视图应该是 F:%s，实际 %v", long, v)
	}

	// 同步也一样：服务器上那个长名字文件夹里的信要收得进来。
	f.host.serves = map[string][]RawMessage{
		"INBOX": {},
		long:    {{UID: 91, Raw: rawMail("long@mid", "长名字文件夹里的")}},
	}
	acct, err := f.svc.ForAccount(ctx, f.tenantID, f.account)
	if err != nil {
		t.Fatal(err)
	}
	f.svc.syncExtraFolders(ctx, SyncConfig{TenantID: f.tenantID, BatchSize: 50, HistoryCap: 500}, acct)
	var n int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM email_inbound
		WHERE tenant_id=$1 AND account_id=$2 AND folder=$3`, f.tenantID, f.account, long).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 { // 挪进来的那封 + 同步进来的那封
		t.Errorf("长名字文件夹里应该有 2 封，实际 %d", n)
	}
}

// 员工在 Foxmail 里把信从收件箱拖进「重要客户」。
//
// 同步这一趟会先从「重要客户」把它收进来，紧接着对账发现收件箱里那封不见了。
// 不跟过去的话，一封信就成了两行：一行在回收站里挂着"消失了"，一行在文件夹里
// 正常显示。垃圾箱那条路上踩过同一个坑，这里是同步自建文件夹之后新开的入口。
func TestMailFiledIntoAFolderElsewhereFollowsInsteadOfDoubling(t *testing.T) {
	f := newFolderFixture(t, 9113)
	ctx := context.Background()
	f.host.folders = []string{"重要客户"}
	mail := f.insertMail(t, "INBOX", 5, "搬走的")
	// 服务器上：收件箱里没有它了，「重要客户」里有，UID 换了。
	f.host.serves = map[string][]RawMessage{
		"INBOX": {},
		"重要客户":  {{UID: 105, Raw: rawMail("搬走的@mid", "搬走的")}},
	}
	// 文件夹是打开邮箱页那一下登记的（前端每次切信箱都会列一次），同步读的是
	// 登记表。真实里两件事一起发生：页面一开，列文件夹和拉列表都会打上来。
	if _, err := f.svc.ListMailFolders(ctx, f.tenantID, f.me, f.account); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SyncMailbox(ctx, SyncConfig{TenantID: f.tenantID, BatchSize: 50, HistoryCap: 500}, f.account); err != nil {
		t.Fatal(err)
	}

	rows, err := f.pool.Query(ctx, `SELECT id, folder, imap_uid, deleted_at IS NOT NULL
		FROM email_inbound WHERE tenant_id=$1 AND message_id='搬走的@mid' ORDER BY id`, f.tenantID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type row struct {
		id      int64
		folder  string
		uid     int64
		deleted bool
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.folder, &r.uid, &r.deleted); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if len(got) != 1 {
		t.Fatalf("一封信应该只有一行，实际 %d 行：%+v", len(got), got)
	}
	if got[0].folder != "重要客户" || got[0].uid != 105 || got[0].deleted {
		t.Errorf("行应该跟到 (重要客户, 105) 且没被删，实际 %+v", got[0])
	}
	if got[0].id != mail {
		t.Errorf("跟过去的该是原来那一行（带着已读、客户关联这些），实际换了新行 %d != %d", got[0].id, mail)
	}
	if v := f.viewsOf(t, got[0].id); len(v) != 1 || v[0] != "F:重要客户" {
		t.Errorf("视图应该是 F:重要客户，实际 %v", v)
	}
}

// 一趟同步最多碰这么多个文件夹，剩下的下一趟排在最前面。
//
// 没有上限的话，建了几十个文件夹的账号会把自己那一格时间片吃光，挤到同一批里
// 别人的箱——分档算出来的余量是按「一个箱多零到三个文件夹」估的。
func TestFolderSyncIsCappedPerPassAndRotates(t *testing.T) {
	f := newFolderFixture(t, 9114)
	ctx := context.Background()
	total := maxFoldersPerPass + 3
	f.host.serves = map[string][]RawMessage{"INBOX": {}}
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("客户%02d", i)
		f.host.folders = append(f.host.folders, name)
		f.host.serves[name] = nil
	}
	if _, err := f.svc.ListMailFolders(ctx, f.tenantID, f.me, f.account); err != nil {
		t.Fatal(err)
	}
	cfg := SyncConfig{TenantID: f.tenantID, BatchSize: 50, HistoryCap: 500}
	synced := func() int {
		t.Helper()
		var n int
		if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_sync_state
			WHERE tenant_id=$1 AND account_id=$2 AND folder LIKE '客户%'`, f.tenantID, f.account).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if _, err := f.svc.SyncMailbox(ctx, cfg, f.account); err != nil {
		t.Fatal(err)
	}
	if n := synced(); n != maxFoldersPerPass {
		t.Errorf("一趟最多 %d 个，实际碰了 %d 个", maxFoldersPerPass, n)
	}
	// 第二趟：这一轮没轮到的排在最前面，所以全都轮到了。
	if _, err := f.svc.SyncMailbox(ctx, cfg, f.account); err != nil {
		t.Fatal(err)
	}
	if n := synced(); n != total {
		t.Errorf("两趟之后应该全都同步过一次（%d），实际 %d", total, n)
	}
}

// 虚拟文件夹的角色不能被后来的一次 LIST 降回去。
//
// 降成 SYSTEM 或 CUSTOM 都会让它开始被同步，而它的内容是别处的信的映射——
// 每封信会按标签存好几遍。判错的代价不对称：反过来只是少列一个文件夹。
func TestVirtualStaysVirtualEvenIfTheNextListLooksOrdinary(t *testing.T) {
	f := newFolderFixture(t, 9115)
	ctx := context.Background()
	f.host.virtual = []string{"全部邮件"}
	got, err := f.svc.ListMailFolders(ctx, f.tenantID, f.me, f.account)
	if err != nil {
		t.Fatal(err)
	}
	roleOfName := func(list []MailFolder, name string) string {
		for _, g := range list {
			if g.Name == name {
				return g.Role
			}
		}
		return ""
	}
	if r := roleOfName(got, "全部邮件"); r != roleVirtual {
		t.Fatalf("带属性时应该是 VIRTUAL，实际 %q", r)
	}
	// 下一次 LIST 服务器没带属性了，名字又不在名单里 —— 算出来会是 CUSTOM。
	f.host.virtual = nil
	f.host.folders = []string{"全部邮件"}
	got, err = f.svc.ListMailFolders(ctx, f.tenantID, f.me, f.account)
	if err != nil {
		t.Fatal(err)
	}
	if r := roleOfName(got, "全部邮件"); r != roleVirtual {
		t.Errorf("VIRTUAL 不该被降回去，实际 %q", r)
	}
}

// 打开邮箱页不该等服务器。
//
// 列文件夹要发一条 IMAP LIST，连接冷的时候还要重新握手加登录——人在页面上
// 要等好几秒才看见左栏，而绝大多数时候答案和库里的一模一样。所以：库里有就
// 立刻回，顺手在后台对一遍；库里一条都没有才非等不可，不然给的是个空左栏，
// 和「这个箱没有文件夹」分不出来。
func TestFolderListAnswersFromTheDatabaseAndRefreshesBehind(t *testing.T) {
	f := newFolderFixture(t, 9116)
	ctx := context.Background()
	f.host.folders = []string{"重要客户"}
	lists := 0
	f.host.onList = func() { lists++ }

	// 第一次：登记表是空的，非等不可，回来就得有内容。
	got, err := f.svc.ListMailFolders(ctx, f.tenantID, f.me, f.account)
	if err != nil {
		t.Fatal(err)
	}
	if lists != 1 {
		t.Errorf("登记表空时应该当场问一次服务器，问了 %d 次", lists)
	}
	found := false
	for _, g := range got {
		if g.Name == "重要客户" {
			found = true
		}
	}
	if !found {
		t.Fatalf("第一次就该看得到服务器上的文件夹：%+v", got)
	}

	// 之后：立刻从库里回，服务器那一趟在后台。
	f.host.folders = append(f.host.folders, "项目B")
	if _, err := f.svc.ListMailFolders(ctx, f.tenantID, f.me, f.account); err != nil {
		t.Fatal(err)
	}
	// 后台那一趟总会落地——只是不在这次请求里。
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err = f.svc.ListMailFolders(ctx, f.tenantID, f.me, f.account)
		if err != nil {
			t.Fatal(err)
		}
		has := false
		for _, g := range got {
			if g.Name == "项目B" {
				has = true
			}
		}
		if has {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("后台那一趟没落地，项目B 一直没出现：%+v", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
