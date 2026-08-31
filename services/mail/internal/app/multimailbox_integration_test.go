package app

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 一个人绑两个信箱之后，两个都得是活的。
//
// 这条钉的是 00043 放开 UNIQUE (tenant_id, employee_id) 之后必须成立的性质。
// 它们今天全都成立，但每一条在改回"按人查"的那一刻都会**静默地**不成立——
// 那正是这一整批改动存在的理由：
//
//	· 两个信箱各有各的同步游标（mail_sync_state 按 account_id 分），
//	  不会一个推进、另一个永远停在 0
//	· 两个信箱各有各的凭据和服务器，跨服务商（263 + Gmail）能并存
//	· 收到的信按 account_id 分开，不会混
//	· 默认信箱只有一个，而且是人选的那个——不是 id 最小的那个
func TestTwoMailboxesForOnePersonBothStayAlive(t *testing.T) {
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

	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	employeeID := tenantID%100000 + 810001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_sync_state WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()

	svc := New(pool, Deps{Secrets: box}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// 同一个人，两个服务商。第一个绑的自动成为默认。
	// 挑服务商，服务器由服务端查表填好——这正是「员工只负责登录」那条路。
	if _, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@sunrise.com", Provider: "p263", Secret: "code-263",
	}); err != nil {
		t.Fatalf("绑第一个信箱：%v", err)
	}
	// 第二个走 UpsertMailAccountShell 那条路——冲突键是地址，所以这是新增
	// 一行，不是把第一个改掉。
	second, err := svc.q.UpsertMailAccountShell(ctx, store.UpsertMailAccountShellParams{
		TenantID: tenantID, EmployeeID: employeeID, Email: "me@gmail.com",
	})
	if err != nil {
		t.Fatalf("绑第二个信箱：%v", err)
	}
	// 密文的 AAD 绑定行 id，所以只能拿到 id 之后再封——和生产代码
	// UpsertMailAccountShell → SetMailAccountSecret 同一个顺序。
	blob, err := box.Seal([]byte("code-gmail"), AccountAAD(tenantID, second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE mail_accounts
		SET smtp_host='smtp.gmail.com', imap_host='imap.gmail.com',
		    secret_enc=$3, key_version=$4
		WHERE tenant_id=$1 AND id=$2`, tenantID, second, blob, box.Version()); err != nil {
		t.Fatal(err)
	}

	boxes, err := svc.q.ListMailAccountsForEmployee(ctx, store.ListMailAccountsForEmployeeParams{
		TenantID: tenantID, EmployeeID: employeeID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(boxes) != 2 {
		t.Fatalf("应该有两个信箱，实际 %d 个——UNIQUE (tenant_id, employee_id) 还在？", len(boxes))
	}

	// 默认只有一个，而且是先绑的那个。**加一个信箱不该悄悄换掉发件人。**
	var defaults int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM mail_accounts
		WHERE tenant_id=$1 AND employee_id=$2 AND is_default`, tenantID, employeeID).Scan(&defaults); err != nil {
		t.Fatal(err)
	}
	if defaults != 1 {
		t.Fatalf("默认信箱应该正好一个，实际 %d 个", defaults)
	}
	firstID, err := svc.defaultAccountIDFor(ctx, tenantID, employeeID)
	if err != nil {
		t.Fatal(err)
	}
	if firstID == second {
		t.Error("加第二个信箱把默认发件人换掉了——写信的发件人不该因为绑了个新箱就变")
	}

	// 各自的凭据和服务器互不串。密文的 AAD 绑定行 id，解对了就证明取对了行。
	a, err := svc.ForAccount(ctx, tenantID, firstID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Email != "me@sunrise.com" || a.Secret != "code-263" {
		t.Errorf("第一个信箱拿到 %s / %q", a.Email, a.Secret)
	}
	b, err := svc.ForAccount(ctx, tenantID, second)
	if err != nil {
		t.Fatal(err)
	}
	if b.Email != "me@gmail.com" || b.IMAPHost != "imap.gmail.com" || b.Secret != "code-gmail" {
		t.Errorf("第二个信箱拿到 %s / %s / %q——跨服务商并存不成立",
			b.Email, b.IMAPHost, b.Secret)
	}

	// 同步游标各走各的。这一条是「第二个箱静默收不到信」的直接反面：
	// 按人查的那一版里，被挑中的箱推进，另一个永远停在 0。
	for _, c := range []struct {
		id  int64
		uid int64
	}{{firstID, 11}, {second, 22}} {
		if _, err := pool.Exec(ctx, `INSERT INTO mail_sync_state
			(tenant_id, account_id, folder, last_uid) VALUES ($1,$2,'INBOX',$3)`,
			tenantID, c.id, c.uid); err != nil {
			t.Fatal(err)
		}
	}
	var uidA, uidB int64
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT last_uid FROM mail_sync_state WHERE tenant_id=$1 AND account_id=$2 AND folder='INBOX'),
		(SELECT last_uid FROM mail_sync_state WHERE tenant_id=$1 AND account_id=$3 AND folder='INBOX')`,
		tenantID, firstID, second).Scan(&uidA, &uidB); err != nil {
		t.Fatal(err)
	}
	if uidA != 11 || uidB != 22 {
		t.Fatalf("两个信箱的同步游标应该各是各的，拿到 %d / %d", uidA, uidB)
	}

	// 改默认：清旧设新是一条语句，中途不会出现零个或两个默认。
	if err := svc.SetDefaultMailbox(ctx, tenantID, employeeID, second); err != nil {
		t.Fatal(err)
	}
	nowDefault, err := svc.defaultAccountIDFor(ctx, tenantID, employeeID)
	if err != nil {
		t.Fatal(err)
	}
	if nowDefault != second {
		t.Errorf("改完默认应该是 %d，拿到 %d", second, nowDefault)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM mail_accounts
		WHERE tenant_id=$1 AND employee_id=$2 AND is_default`, tenantID, employeeID).Scan(&defaults); err != nil {
		t.Fatal(err)
	}
	if defaults != 1 {
		t.Fatalf("改完默认之后仍该正好一个，实际 %d 个", defaults)
	}
}

// 同一条会话落在两个信箱里，是两行，不是一行。
//
// 这一条钉的是 00044。客户把同一封询价抄送到公司的 263 和业务员的 Gmail，
// 两个信箱各收到一封，thread_key 相同（同一个 Message-ID 链）。按 00034 的
// 老口径，(租户, 人, 会话, 视图) 会把两封合成一行：列表上「客户回了 2 次」，
// 点进去是两个信箱混在一起的两封，而左侧切到哪个信箱都是同一行。
//
// **这个错不会报任何错。** 行数对得上，约束不违反，日志里一个字都没有——
// 所以只能靠一条断言把口径钉死。
//
// 顺带钉住两件同批做的事：
//
//	· 归档只归档这个信箱那一份（SetThreadFlags 带 account_id）。不带的话，
//	  在 263 点归档会把 Gmail 那份也归档，触发器随后把两行都刷新，表里
//	  完全自洽——症状是「另一个信箱的信自己不见了」。
//	· 计数按 (信箱, 会话) 去重，和列表同一个口径。
func TestOneConversationInTwoMailboxesIsTwoRows(t *testing.T) {
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

	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	employeeID := tenantID%100000 + 830001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_thread_view WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()

	svc := New(pool, Deps{Secrets: box}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	bind := func(email string) int64 {
		t.Helper()
		id, err := svc.q.UpsertMailAccountShell(ctx, store.UpsertMailAccountShellParams{
			TenantID: tenantID, EmployeeID: employeeID, Email: email,
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	work, personal := bind("me@sunrise.com"), bind("me@gmail.com")

	// 同一个 thread_key，两个信箱各一封。imap_uid 各自从 1 开始——UID 是
	// 每信箱独立的，这里刻意撞上，顺便证明唯一约束是按信箱算的。
	const thread = "thr-same-conversation"
	insert := func(accountID int64, uid int64, subject string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, message_id, thread_key, folder,
			 imap_uid, from_email, subject, body_text, received_at)
			VALUES ($1,$2,$3,$4,$5,'INBOX',$6,'buyer@overseas.com',$7,'hi',now())
			RETURNING id`, tenantID, accountID, employeeID,
			subject+"@overseas.com", thread, uid, subject).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	workMail := insert(work, 1, "询价-发到公司箱")
	insert(personal, 1, "询价-抄送到私人箱")

	countRows := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM mail_thread_view
			WHERE tenant_id=$1 AND owner_id=$2 AND view='INBOX'`,
			tenantID, employeeID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := countRows(); n != 2 {
		t.Fatalf("两个信箱收到同一条会话，会话表里应该是 2 行，实际 %d 行——"+
			"1 行说明还在按 (租户,人,会话,视图) 合并，两个信箱的信被并成了一条", n)
	}
	// 每一行只算自己信箱那一封。合并的话这里会是 2。
	var counts []int32
	rows, err := pool.Query(ctx, `SELECT msg_count FROM mail_thread_view
		WHERE tenant_id=$1 AND owner_id=$2 AND view='INBOX' ORDER BY account_id`,
		tenantID, employeeID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var c int32
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		counts = append(counts, c)
	}
	rows.Close()
	for _, c := range counts {
		if c != 1 {
			t.Errorf("每个信箱各收到一封，msg_count 应该是 1，实际 %v——"+
				"列表上会显示「客户回了 2 次」", counts)
			break
		}
	}

	// 在公司箱归档，私人箱那一份必须原地不动。
	yes := true
	if err := svc.MarkInbound(ctx, tenantID, employeeID, workMail,
		nil, nil, &yes, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	var archivedWork, archivedPersonal int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM email_inbound
		  WHERE tenant_id=$1 AND account_id=$2 AND archived_at IS NOT NULL),
		(SELECT count(*) FROM email_inbound
		  WHERE tenant_id=$1 AND account_id=$3 AND archived_at IS NOT NULL)`,
		tenantID, work, personal).Scan(&archivedWork, &archivedPersonal); err != nil {
		t.Fatal(err)
	}
	if archivedWork != 1 {
		t.Errorf("公司箱那封应该归档了，实际归档 %d 封", archivedWork)
	}
	if archivedPersonal != 0 {
		t.Error("在公司箱点归档，把私人箱那封也归档了——两个信箱是两条会话，" +
			"而这个错不报任何错，用户只会发现「另一个信箱的信自己不见了」")
	}

	// 点开会话读到的，也只该是这个信箱那一份。
	//
	// 列表行上写着 (1)，因为 msg_count 是按信箱算的；打开时若不限定信箱，
	// 读到的是两封——**列表和详情自相矛盾**，而这个矛盾是 00044 自己造出来
	// 的，不是历史遗留：加维度只加了一半才会这样。
	items, err := svc.GetMailThread(ctx, tenantID, employeeID, workMail, thread)
	if err != nil {
		t.Fatal(err)
	}
	in := 0
	for _, it := range items {
		if it.Direction == "IN" {
			in++
		}
	}
	if in != 1 {
		t.Errorf("从公司箱点进去，收到的信应该只有 1 封，实际 %d 封——"+
			"列表行写着 (1)，点开却是两个信箱的信合在一起", in)
	}

	// 不说明从哪封点进来（旧前端就是这么发的），仍然读全部——部署顺序是
	// 后端先发、前端后发，这条路必须留着，否则那几分钟里会话是空的。
	all, err := svc.GetMailThread(ctx, tenantID, employeeID, 0, thread)
	if err != nil {
		t.Fatal(err)
	}
	inAll := 0
	for _, it := range all {
		if it.Direction == "IN" {
			inAll++
		}
	}
	if inAll != 2 {
		t.Errorf("旧前端不带信件 id，应该照旧读全部（2 封），实际 %d 封——"+
			"后端先上线的那几分钟里，会话会是空的", inAll)
	}
}

// 一家 263 的公司里，员工绑一个 Gmail，绑得上而且能用。
//
// 这是整批改动要解锁的那件事，也是从前**做不到**的：地址只能来自登录令牌
// （所以永远是公司域名的），收发服务器一家公司只有一份（所以拿 Gmail 的
// 账号去登的是 imap.263.net，报的是「认证失败」，指向的是密码错）。
//
// 顺带钉住绑定入口的三条新约束：
//
//	· 归属只来自调用方给的 employeeID，绑到自己名下
//	· 服务器由服务端按服务商代号查表，不是调用方说了算
//	· 每一次绑定留痕
func TestSomebodyAtA263CompanyCanBindTheirGmail(t *testing.T) {
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
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	employeeID := tenantID%100000 + 840001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_hosts WHERE tenant_id=$1", tenantID)
	}()
	// 公司是 263 的。
	if _, err := pool.Exec(ctx, `INSERT INTO mail_hosts
		(tenant_id, domain, smtp_host, smtp_port, smtp_security, imap_host, imap_port, imap_security)
		VALUES ($1,'sunrise.com','smtp.263.net',465,'SSL','imap.263.net',993,'SSL')`,
		tenantID); err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Secrets: box}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// 公司信箱：不挑服务商，落回公司配置。
	work, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "lina@sunrise.com", Secret: "code-263",
	})
	if err != nil {
		t.Fatalf("绑公司信箱：%v", err)
	}
	// 私人 Gmail：地址后缀就认得出来，不用挑也不用填服务器。
	personal, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "Lina.Personal@Gmail.com", Secret: "app-password",
	})
	if err != nil {
		t.Fatalf("在一家 263 的公司里绑 Gmail：%v", err)
	}
	if personal.AccountID == work.AccountID {
		t.Fatal("绑第二个信箱把第一个改掉了——那是「一人一箱」的老语义")
	}
	// 地址规范化过：请求里写的是混合大小写，落库的是小写。界面要显示的是
	// 落库的那个，所以它必须回来。
	if personal.Email != "lina.personal@gmail.com" {
		t.Errorf("回来的地址是 %q，应该是规范化之后的那个", personal.Email)
	}

	a, err := svc.ForAccount(ctx, tenantID, work.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.ForAccount(ctx, tenantID, personal.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if a.IMAPHost != "imap.263.net" {
		t.Errorf("公司信箱该走 imap.263.net，拿到 %s", a.IMAPHost)
	}
	if b.IMAPHost != "imap.gmail.com" || b.Host != "smtp.gmail.com" {
		t.Errorf("Gmail 信箱该走 Gmail 的服务器，拿到 %s / %s——"+
			"这正是从前做不到的那件事：一家公司只有一份收发服务器配置",
			b.Host, b.IMAPHost)
	}
	if a.Secret != "code-263" || b.Secret != "app-password" {
		t.Errorf("两个信箱的授权码串了：%q / %q", a.Secret, b.Secret)
	}
	// 先绑的仍是默认，加一个信箱不该悄悄换掉发件人。
	if !boxIsDefault(t, ctx, pool, tenantID, work.AccountID) {
		t.Error("加了第二个信箱之后默认发件人被换掉了")
	}

	// 同一个地址换个大小写再绑一次，是**更新同一行**，不是新增。
	//
	// 这条钉的是 00046：地址一律以小写存，而 ON CONFLICT (tenant_id, email)
	// 是大小写敏感的。不规范化的话 'Me@x.com' 和 'me@x.com' 在唯一约束眼里
	// 是两个值，同一个信箱裂成两行——两份凭据、两份同步游标、两套已收邮件，
	// 而且一声不吭。
	again, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "LINA.PERSONAL@GMAIL.COM", Secret: "app-password-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.AccountID != personal.AccountID {
		t.Errorf("同一个地址换个大小写又开了一个信箱（%d ≠ %d）——"+
			"两份凭据两份游标，而唯一约束一声不吭",
			again.AccountID, personal.AccountID)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM mail_accounts
		WHERE tenant_id=$1 AND employee_id=$2`, tenantID, employeeID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Errorf("应该只有两个信箱（公司 + Gmail），实际 %d 个", rows)
	}

	// 两次绑定各留一行痕，记的是落库后的地址。
	var logged int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM mail_binding_log
		WHERE tenant_id=$1 AND employee_id=$2 AND action='BIND'`,
		tenantID, employeeID).Scan(&logged); err != nil {
		t.Fatal(err)
	}
	if logged != 2 {
		t.Errorf("两次绑定该留两行痕，实际 %d 行——地址交还给调用方之后，"+
			"「谁绑了什么」不再有一个不言自明的答案", logged)
	}
}

func boxIsDefault(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, accountID int64) bool {
	t.Helper()
	var yes bool
	if err := pool.QueryRow(ctx, `SELECT is_default FROM mail_accounts
		WHERE tenant_id=$1 AND id=$2`, tenantID, accountID).Scan(&yes); err != nil {
		t.Fatal(err)
	}
	return yes
}

// 别人已经绑了的地址，第二个人绑不上——UNIQUE (tenant_id, email) 是 00043
// 有意留下的那一条，而撞上时要说人话，不是抛 23505。
func TestOneMailboxCannotBelongToTwoPeople(t *testing.T) {
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
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	empA, empB := tenantID%100000+820001, tenantID%100000+820002
	if _, err := svc.VerifyMailSecret(ctx, tenantID, empA, BindRequest{
		Email: "shared@sunrise.com", Provider: "p263", Secret: "x",
	}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.VerifyMailSecret(ctx, tenantID, empB, BindRequest{
		Email: "shared@sunrise.com", Provider: "p263", Secret: "y",
	})
	if err == nil {
		t.Fatal("两个人绑同一个地址竟然成功了——那封信该算谁的？")
	}
	if !strings.Contains(err.Error(), "已经被") {
		t.Errorf("撞上时要说人话，拿到：%v", err)
	}
}
