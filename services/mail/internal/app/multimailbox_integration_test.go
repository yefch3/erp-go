package app

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
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

// 在哪个箱里写信，就从哪个箱发出去。
//
// 这是「我视图在 263，默认发件箱是 Gmail，点发送从哪个发出去」那个问题的
// 答案。改动之前答案是 **Gmail**——出站队列只记 sender_id，发信那一刻拿它
// 反查回这个人的默认箱。
//
// 跟着一起错的还有三样：用哪台 SMTP 服务器、已发送副本存进哪个箱，以及
// 最阴的那条——一封排队中的信**重试时才反查**，这期间换过默认箱的话，
// 重试会从另一个地址发出去，同一封信两次尝试两个发件人。
//
// 所以从哪个箱发是**入队时定死**的，不是发信时算的。这条测试钉的就是那个
// 「定死」：入队之后把默认箱换掉，队列里那封信的归属一动不动。
func TestAMessageRemembersWhichMailboxItLeavesFrom(t *testing.T) {
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
	employeeID := tenantID%100000 + 860001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_messages WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_campaigns WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	// 发活动要编号生成器。这里只要它给出一个不重复的串，不测编号规则本身。
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	work, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code-qq",
	})
	if err != nil {
		t.Fatal(err)
	}
	personal, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@163.com", Provider: "netease163", Secret: "code-163",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 默认是先绑的那个（QQ）。下面要证的正是「默认是什么不影响这封信」。
	if def, _ := svc.defaultAccountIDFor(ctx, tenantID, employeeID); def != work.AccountID {
		t.Fatalf("默认该是先绑的 QQ，拿到 %d", def)
	}

	// 点名从 163 那个箱发。
	if _, err := svc.CreateCampaign(ctx, tenantID, CampaignInput{
		Subject: "报价", Body: "hi", Format: FormatText, Kind: "TRANSACTIONAL",
		AccountID:  personal.AccountID,
		Recipients: []Recipient{{Email: "buyer@overseas.com", Name: "Buyer"}},
	}, Operator{ID: employeeID, Name: "业务员"}); err != nil {
		t.Fatalf("发信：%v", err)
	}
	var got int64
	if err := pool.QueryRow(ctx, `SELECT account_id FROM email_messages
		WHERE tenant_id=$1 ORDER BY id DESC LIMIT 1`, tenantID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != personal.AccountID {
		t.Fatalf("点名从 163 发，队列里记的却是 %d（163 是 %d，QQ 是 %d）——"+
			"这就是「视图在 A、默认在 B，结果从 B 发出去」那个毛病",
			got, personal.AccountID, work.AccountID)
	}

	// **入队之后把默认箱换掉，这封信一动不动。**
	//
	// 反查那一版在这里就变了：重试时才算，算出来的是新的默认箱。
	if err := svc.SetDefaultMailbox(ctx, tenantID, employeeID, personal.AccountID); err != nil {
		t.Fatal(err)
	}
	var still int64
	if err := pool.QueryRow(ctx, `SELECT account_id FROM email_messages
		WHERE tenant_id=$1 ORDER BY id DESC LIMIT 1`, tenantID).Scan(&still); err != nil {
		t.Fatal(err)
	}
	if still != personal.AccountID {
		t.Error("换了默认箱之后，排队中那封信的发件箱跟着变了")
	}

	// 不点名时：回复继承**收到原信的那个箱**。对方写到哪个地址，回信就该从
	// 哪个地址出去，否则会话在客户那边会断成两条。
	var inboundID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, subject, body_text, received_at)
		VALUES ($1,$2,$3,'ask@overseas.com','thr-1','INBOX',1,'buyer@overseas.com','询价','hi',now())
		RETURNING id`, tenantID, work.AccountID, employeeID).Scan(&inboundID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCampaign(ctx, tenantID, CampaignInput{
		Subject: "Re: 询价", Body: "hi", Format: FormatText, Kind: "TRANSACTIONAL",
		ReplyToInboundID: inboundID,
		Recipients:       []Recipient{{Email: "buyer@overseas.com", Name: "Buyer"}},
	}, Operator{ID: employeeID, Name: "业务员"}); err != nil {
		t.Fatalf("回复：%v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT account_id FROM email_messages
		WHERE tenant_id=$1 ORDER BY id DESC LIMIT 1`, tenantID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != work.AccountID {
		t.Errorf("回复该从收到原信的那个箱（QQ=%d）发，拿到 %d——"+
			"此刻默认箱是 163，说明它又去查默认箱了", work.AccountID, got)
	}

	// 别人的信箱借不到：填一个不属于自己的 account_id 要被拒。
	other := tenantID%100000 + 860002
	stranger, err := svc.VerifyMailSecret(ctx, tenantID, other, BindRequest{
		Email: "someone@qq.com", Provider: "qq", Secret: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCampaign(ctx, tenantID, CampaignInput{
		Subject: "冒名", Body: "hi", Format: FormatText, Kind: "TRANSACTIONAL",
		AccountID:  stranger.AccountID,
		Recipients: []Recipient{{Email: "buyer@overseas.com", Name: "Buyer"}},
	}, Operator{ID: employeeID, Name: "业务员"}); err == nil {
		t.Error("拿别人的信箱 id 发信成功了——那等于以同事的地址给客户写信")
	}
}

// 已发送也按信箱分，而且**两条腿一起分**。
//
// 已发送这一页是两个来源拼起来的：邮件服务器自己存的 SENT 副本，和 ERP 的
// 发送记录（服务器没留副本时的兜底）。前者一直带着 account_id，后者到 00047
// 才有。
//
// 这条测试钉的是「一起」。只筛一条腿是这里最容易犯、也最难看出来的错：切到
// 163 箱，看到的是 163 的已发送**加上**从 QQ 箱发出去的那些——一半对一半不
// 对，行数不少、不报错，而且两种行长得一模一样。
//
// 顺带钉住 00047 之前那些 account_id=0 的历史行：它们只在「不筛」时出现。
// 把它们塞进任何一个箱都是猜的，猜错的样子是「这封信不是我从这个地址发的」。
func TestSentIsPerMailboxOnBothLegs(t *testing.T) {
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
	employeeID := tenantID%100000 + 870001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_messages WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	work, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code-qq",
	})
	if err != nil {
		t.Fatal(err)
	}
	personal, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@163.com", Provider: "netease163", Secret: "code-163",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 第一条腿：服务器留下的 SENT 副本，两个箱各一封。
	sentCopy := func(acct int64, uid int32, subject string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
			 from_email, to_email, subject, snippet, body_text, received_at, sent_at)
			VALUES ($1,$2,$3,$4,$5,'SENT',$6,'me@x','buyer@overseas.com',$7,'…','…',now(),now())`,
			tenantID, acct, employeeID, subject+"-mid", subject+"-thr", uid, subject); err != nil {
			t.Fatal(err)
		}
	}
	sentCopy(work.AccountID, 1, "QQ 发出去的")
	sentCopy(personal.AccountID, 2, "163 发出去的")

	// 第二条腿：ERP 的发送记录，服务器没留副本（没有对应的 email_inbound 行）。
	// sent_at 要早于宽限期，否则查询按「副本还在路上」把它藏起来。
	erpOnly := func(acct int64, from, subject string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO email_messages
			(tenant_id, message_key, sender_id, account_id, to_email, subject, body,
			 from_email, status, sent_at)
			VALUES ($1, gen_random_uuid(), $2, $3, 'buyer@overseas.com', $4, 'hi',
			        $5, 'DELIVERED', now() - interval '1 hour')`,
			tenantID, employeeID, acct, subject, from); err != nil {
			t.Fatal(err)
		}
	}
	erpOnly(work.AccountID, "me@qq.com", "QQ 的发送记录")
	erpOnly(personal.AccountID, "me@163.com", "163 的发送记录")
	// 00047 之前入队的：account_id 还是 0。
	erpOnly(0, "me@qq.com", "上古发送记录")

	subjects := func(accountID int64) []string {
		t.Helper()
		page, err := svc.ListMailboxSent(ctx, tenantID, employeeID, accountID, "", "", 50, ListSort{})
		if err != nil {
			t.Fatal(err)
		}
		// 分页器要和列表说同一件事：数字对不上的话，翻到第二页是空的。
		if int(page.Total) != len(page.Mails) {
			t.Errorf("账号 %d：列表 %d 行，计数说 %d——两条 SQL 的筛选口径不一致",
				accountID, len(page.Mails), page.Total)
		}
		out := make([]string, 0, len(page.Mails))
		for _, m := range page.Mails {
			out = append(out, m.Subject)
		}
		return out
	}
	has := func(list []string, want string) bool {
		for _, s := range list {
			if s == want {
				return true
			}
		}
		return false
	}

	got := subjects(personal.AccountID)
	if !has(got, "163 发出去的") {
		t.Errorf("切到 163 箱，看不到 163 的已发送副本：%v", got)
	}
	if !has(got, "163 的发送记录") {
		t.Errorf("切到 163 箱，看不到 163 的 ERP 发送记录：%v", got)
	}
	if has(got, "QQ 发出去的") {
		t.Errorf("切到 163 箱，却看到了 QQ 箱的已发送副本——服务器副本那条腿没筛：%v", got)
	}
	if has(got, "QQ 的发送记录") {
		t.Errorf("切到 163 箱，却看到了从 QQ 发出去的 ERP 记录——"+
			"ERP 那条腿没筛，这一页就是一半对一半不对：%v", got)
	}
	if has(got, "上古发送记录") {
		t.Errorf("account_id=0 的历史行落进了 163 箱——那是猜的，"+
			"而猜错的样子是「这封信不是我从这个地址发的」：%v", got)
	}

	// 不筛（旧令牌、一个箱都没绑的人）：全都在，历史行也在。
	all := subjects(0)
	for _, want := range []string{"QQ 发出去的", "163 发出去的",
		"QQ 的发送记录", "163 的发送记录", "上古发送记录"} {
		if !has(all, want) {
			t.Errorf("不筛的时候少了「%s」：%v", want, all)
		}
	}
}

// 立即收信 收的是**眼前这个箱**，而且认的是账号号不是员工号。
//
// 这一条钉的是一个从第一期漏到现在的真 bug。SyncMailboxInteractive 的第三个
// 参数在第一期从「员工号」改成了「账号号」，SyncNow 这个调用点没跟着改——
// 两个都是 int64，**编译器一声不吭**。于是 立即收信 拿员工号去 mail_accounts
// 里当 id 查：
//
//	· 查不到 → 点了没反应，前端一个字都不说
//	· 查得到 → 收的是**同事的信箱**，花同事的配额、动同事的已读状态
//
// 测试靠 id 空间错开来分辨这两个数：员工号是 87xxxx 量级，账号号是小序列。
// 把解析改回「用员工号」的话，第一条断言会翻成「这个邮箱不在你名下」。
func TestSyncNowTakesAMailboxNotAnEmployee(t *testing.T) {
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
	employeeID := tenantID%100000 + 880001
	colleague := tenantID%100000 + 880002
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	mine, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "mine@qq.com", Provider: "qq", Secret: "code-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := svc.VerifyMailSecret(ctx, tenantID, colleague, BindRequest{
		Email: "theirs@qq.com", Provider: "qq", Secret: "code-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if mine.AccountID == employeeID {
		t.Skip("账号号正好撞上员工号，这次分辨不了两者")
	}

	// 点名收自己的箱：认下来了，走到「没有邮件通道」为止（测试里 mailbox 是
	// 空的）。这是「解析成功」的信号。
	//
	// 漏改的那一版在这里就红：它拿员工号去查 mail_accounts，查不到。
	if _, _, err := svc.SyncNow(ctx, tenantID, employeeID, mine.AccountID); err != ErrMailHostNotConfigured {
		t.Fatalf("点名收自己的箱应该认下来，拿到 %v——"+
			"「不在你名下」说明它查的不是账号号", err)
	}

	// 不点名：收默认箱。旧前端走这条，行为要和从前一样。
	if _, _, err := svc.SyncNow(ctx, tenantID, employeeID, 0); err != ErrMailHostNotConfigured {
		t.Errorf("不点名该退回默认箱，拿到 %v", err)
	}

	// 同事的箱收不了。不拦的话，点一下就把同事的信箱拉了一遍。
	if _, _, err := svc.SyncNow(ctx, tenantID, employeeID, theirs.AccountID); err == nil ||
		err == ErrMailHostNotConfigured {
		t.Errorf("收了同事的信箱，err=%v", err)
	}

	// 一个箱都没绑：说「没有邮箱账号」，不是「邮件服务没配」——后者会把人
	// 送去找管理员，而该做的是先绑一个箱。
	nobody := tenantID%100000 + 880003
	if _, _, err := svc.SyncNow(ctx, tenantID, nobody, 0); err != ErrNoMailAccount {
		t.Errorf("一个箱都没绑时该说没有邮箱账号，拿到 %v", err)
	}
}

// 配额记在这封信真正用的那个箱上，不是默认箱。
//
// 限额是每家服务商自己的：263 的套餐上限和 Gmail 的 500/天不是一个量纲。
// 记错箱之后两头都坏，而且都不报错：
//
//   - 从 163 发的一百封全记在 QQ 的计数上 → 163 自己的上限一辈子不生效，
//     等于拿默认箱的额度往严格的那家灌，直到服务商自己动手
//   - 第 101 封被 QQ 的上限拦下来，而提示里写的是一个 QQ 从没达到过的数字
//
// 这条测试的红验证是把解析改回 defaultAccountIDFor：第一段断言会看到计数
// 落在 QQ 上。
func TestQuotaCountsAgainstTheMailboxTheMailActuallyLeft(t *testing.T) {
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
	employeeID := tenantID%100000 + 890001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_send_counters WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// QQ 先绑 → 它是默认箱。下面证明「默认是哪个」不影响这件事。
	work, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code-qq",
	})
	if err != nil {
		t.Fatal(err)
	}
	personal, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@163.com", Provider: "netease163", Secret: "code-163",
	})
	if err != nil {
		t.Fatal(err)
	}

	counted := func(accountID int64) int32 {
		t.Helper()
		var n int32
		if err := pool.QueryRow(ctx, `SELECT coalesce(sum(sent_count),0)::int
			FROM mail_send_counters WHERE tenant_id=$1 AND account_id=$2`,
			tenantID, accountID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// 从 163 发三封。记在 163 上，QQ 一封都不该有。
	for i := 0; i < 3; i++ {
		svc.countSend(ctx, tenantID, employeeID, personal.AccountID)
	}
	if got := counted(personal.AccountID); got != 3 {
		t.Errorf("163 上该记 3 封，实际 %d", got)
	}
	if got := counted(work.AccountID); got != 0 {
		t.Errorf("从 163 发的信记到了默认的 QQ 上（%d 封）——"+
			"163 自己的上限就永远不会生效", got)
	}

	// 163 的上限设成 3。到顶的是 163，不是默认箱。
	if _, err := pool.Exec(ctx, `UPDATE mail_accounts SET hourly_quota=3
		WHERE tenant_id=$1 AND id=$2`, tenantID, personal.AccountID); err != nil {
		t.Fatal(err)
	}
	if wait, reason := svc.overQuota(ctx, tenantID, employeeID, personal.AccountID); wait <= 0 {
		t.Error("163 已经到上限了，还放行——限额没跟着信箱走")
	} else if !strings.Contains(reason, "3") {
		t.Errorf("提示里该是 163 自己的上限 3，实际说的是 %q", reason)
	}
	// 同一个人、同一时刻，从 QQ 发不受影响：两个箱两本账。
	if wait, _ := svc.overQuota(ctx, tenantID, employeeID, work.AccountID); wait > 0 {
		t.Error("163 到上限把 QQ 也一起拦住了——两个箱共用了一本账")
	}

	// 0 = 00047 之前入队的行，退回默认箱（QQ）。行为和不做这次改动时一样。
	svc.countSend(ctx, tenantID, employeeID, 0)
	if got := counted(work.AccountID); got != 1 {
		t.Errorf("account_id=0 的老行该退回默认箱，QQ 上拿到 %d", got)
	}
}

// seqNumbers 是测试用的编号生成器：只保证不重复。
type seqNumbers struct{ n atomic.Int64 }

func (s *seqNumbers) Next(context.Context, string) (string, error) {
	return "T-" + strconv.FormatInt(s.n.Add(1), 10), nil
}

// 左侧那排角标，每个箱一个数，口径必须和收件箱顶上那个总数**一模一样**。
//
// 两条查询回答的是同一个问题（这个箱有多少封没读），只是一条按箱分组、
// 一条只算一个箱。口径一旦分家，症状是「角标写着 3，切过去一封都没有」——
// 而人会以为信丢了，比没有角标糟得多。
//
// 所以这条测试不去断言具体数字，它断言**两条查询对每个箱都给出同一个数**，
// 而且是在各种该排除的行都摆好之后：归档过的、删掉的、退信、垃圾箱里没被
// 捞回来的、以及已经读过的。
func TestPerMailboxUnreadAgreesWithTheInboxCount(t *testing.T) {
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
	employeeID := tenantID%100000 + 900001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	work, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code-qq",
	})
	if err != nil {
		t.Fatal(err)
	}
	personal, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@163.com", Provider: "netease163", Secret: "code-163",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 每一封只在「普通的那封」之外多带一列，那一列就是它该被排除的理由。
	// 列名和它的值绑在一起（valuesFor），免得加一列时忘了加对应的字面量——
	// 那种错插进去不报错，只是这封信算进了不该算的那一档。
	uid := int64(0)
	add := func(acct int64, folder, extraCol string) {
		t.Helper()
		uid++
		_, err := pool.Exec(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
			 from_email, subject, body_text, received_at`+extraCol+`)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'buyer@overseas.com','询价','hi',now()`+
			valuesFor(extraCol)+`)`,
			tenantID, acct, employeeID,
			"m"+strconv.FormatInt(uid, 10)+"@x", "thr"+strconv.FormatInt(uid, 10),
			folder, uid)
		if err != nil {
			t.Fatal(err)
		}
	}
	// QQ：两封该算，其余每一封都该被排除掉一条规则。
	add(work.AccountID, "INBOX", "")
	add(work.AccountID, "INBOX", "")
	add(work.AccountID, "INBOX", ", is_read")     // 读过了
	add(work.AccountID, "INBOX", ", is_bounce")   // 退信
	add(work.AccountID, "INBOX", ", archived_at") // 归档过
	add(work.AccountID, "INBOX", ", deleted_at")  // 删掉了
	add(work.AccountID, "JUNK", "")               // 垃圾箱，没捞回来
	add(work.AccountID, "SENT", "")               // 已发送不算未读
	// 163：一封普通的，加一封从垃圾箱捞回来的——那一封**要算**。
	add(personal.AccountID, "INBOX", "")
	add(personal.AccountID, "JUNK", ", not_junk")

	perBox, err := svc.q.CountUnreadByMailbox(ctx, store.CountUnreadByMailboxParams{
		TenantID: tenantID, OwnerID: employeeID,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]int64{}
	for _, r := range perBox {
		got[r.AccountID] = r.Unread
	}

	for _, acct := range []int64{work.AccountID, personal.AccountID} {
		one, err := svc.q.CountUnread(ctx, store.CountUnreadParams{
			TenantID: tenantID, OwnerID: employeeID, AccountID: &acct,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got[acct] != one {
			t.Errorf("信箱 %d：角标那条查询说 %d，收件箱那条说 %d——"+
				"两个口径分家了，症状是「角标写着 N，切过去一封都没有」",
				acct, got[acct], one)
		}
	}
	// 顺带把数字本身钉住，免得两条查询一起错还互相印证。
	if got[work.AccountID] != 2 {
		t.Errorf("QQ 该有 2 封未读，拿到 %d", got[work.AccountID])
	}
	if got[personal.AccountID] != 2 {
		t.Errorf("163 该有 2 封未读（含一封从垃圾箱捞回来的），拿到 %d", got[personal.AccountID])
	}
}

// valuesFor 给上面那个 add 拼出和列名对应的字面量。
//
// 只认它用到的那几列——测试里的辅助函数不需要通用，需要的是看一眼就知道
// 它在插什么。
func valuesFor(cols string) string {
	switch cols {
	case "":
		return ""
	case ", is_read", ", is_bounce", ", not_junk":
		return ", TRUE"
	case ", archived_at", ", deleted_at":
		return ", now()"
	}
	panic("valuesFor: 不认识的列 " + cols)
}

// 「有人在看这个箱」这个时间戳，半分钟内只落一次。
//
// 邮件页每次翻页、每次开信都会打到那条路径上。逐次写等于把一次读变成一次
// 写，而这个值是拿来排班的（下一批按它分档：有人看的箱全量同步，没人看的
// 只刷未读数），半分钟的粒度绰绰有余。
func TestMailboxReadTimeIsThrottled(t *testing.T) {
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
	employeeID := tenantID%100000 + 910001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	mine, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code-qq",
	})
	if err != nil {
		t.Fatal(err)
	}

	readAt := func() (time.Time, bool) {
		t.Helper()
		var at *time.Time
		if err := pool.QueryRow(ctx, `SELECT last_read_at FROM mail_accounts
			WHERE tenant_id=$1 AND id=$2`, tenantID, mine.AccountID).Scan(&at); err != nil {
			t.Fatal(err)
		}
		if at == nil {
			return time.Time{}, false
		}
		return *at, true
	}

	// 绑定时还没人看过。
	if _, ok := readAt(); ok {
		t.Fatal("刚绑好的箱就有「上次查看」时间")
	}

	svc.touchMailboxRead(ctx, tenantID, mine.AccountID)
	first, ok := readAt()
	if !ok {
		t.Fatal("看了一次之后仍然没有时间戳")
	}

	// 紧接着再看十次——一次都不该再写。
	for i := 0; i < 10; i++ {
		svc.touchMailboxRead(ctx, tenantID, mine.AccountID)
	}
	again, _ := readAt()
	if !again.Equal(first) {
		t.Errorf("半分钟内又写了：%v → %v。翻一页写一次的话，"+
			"这张表会被读操作按写操作的频率打", first, again)
	}

	// 把时间拨回一分钟前，下一次就该写了——证明限的是频率，不是「只写一次」。
	if _, err := pool.Exec(ctx, `UPDATE mail_accounts SET last_read_at = now() - interval '1 minute'
		WHERE tenant_id=$1 AND id=$2`, tenantID, mine.AccountID); err != nil {
		t.Fatal(err)
	}
	svc.touchMailboxRead(ctx, tenantID, mine.AccountID)
	after, _ := readAt()
	if !after.After(first.Add(-time.Second)) || after.Before(first) {
		t.Errorf("过了限频窗口还是没写：%v", after)
	}
}

// 分档只改延迟，不改对错。
//
// 这是整个分档设计的**唯一支柱**：没人在看的信箱不再每轮全量同步，而是每
// 十分钟问一条 STATUS；看见有我们没取过的信，那一轮就把它提上来全量同步。
// 所以最坏是「晚十分钟」，不是「收不到」。
//
// worthAFullSync 就是那个判断。它判错的样子是**一个信箱安静地不再收信**——
// 没有报错、没有日志、页面上一切正常，只是客户的询价再也不来。所以每一条
// 分支都要钉住，尤其是「判不准」的那几条：它们必须一律回「同步」。
func TestAnIdleMailboxWithNewMailStillGetsFetched(t *testing.T) {
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
	employeeID := tenantID%100000 + 920001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_sync_state WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	cfg := SyncConfig{TenantID: tenantID}.withDefaults()

	mine, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code-qq",
	})
	if err != nil {
		t.Fatal(err)
	}
	acct, err := svc.ForAccount(ctx, tenantID, mine.AccountID)
	if err != nil {
		t.Fatal(err)
	}

	// 已经取到第 10 封。
	if _, err := pool.Exec(ctx, `INSERT INTO mail_sync_state
		(tenant_id, account_id, folder, last_uid) VALUES ($1,$2,'INBOX',10)`,
		tenantID, mine.AccountID); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		st   FolderStatus
		want bool
		why  string
	}{
		{
			name: "服务器说下一封是 11——正好是我们取到的下一封，没有新信",
			st:   FolderStatus{UIDNext: 11, Unseen: 0},
			want: false,
			why:  "没有新信却每轮都全量同步，分档就白做了",
		},
		{
			// 生产里最常见的一格：客户回了**一封**。差一写错的话，恰恰就是
			// 这一格被判成「没有新信」——多封的情况照常工作，于是这个 bug
			// 只在最普通的场景下出现。
			//
			// Unseen 故意和我们库里的数一致（都是 0），这样这一格**只**由
			// UIDNext 那条判断决定。不这样的话未读数那条兜底会把差一盖住，
			// 测试照绿——而 UIDNext 那条其实已经错了。
			name: "服务器说下一封是 12——正好来了一封，而且未读数看不出差别",
			st:   FolderStatus{UIDNext: 12, Unseen: 0},
			want: true,
			why: "**这一条错了就是信收不到**，错的还是最常见的那一格（客户回了一封）；" +
				"而且未读数那条兜底在这一格帮不上忙",
		},
		{
			name: "服务器说下一封是 15——中间有四封我们没取过",
			st:   FolderStatus{UIDNext: 15, Unseen: 0},
			want: true,
			why:  "**这一条错了就是信收不到**：有新信而没被提上来同步",
		},
		{
			name: "没有新信，但服务器数出 3 封未读而我们库里是 0",
			st:   FolderStatus{UIDNext: 11, Unseen: 3},
			want: true,
			why:  "有人在别的客户端上动过已读状态，跟一次",
		},
		{
			name: "服务器没回 UIDNEXT——什么都判不出来",
			st:   FolderStatus{UIDNext: 0, Unseen: 0},
			want: true,
			why: "判不准必须同步。省掉这一条的话，一台不回 UIDNEXT 的主机上" +
				"所有没人看的箱都会安静地不再收信",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := svc.worthAFullSync(ctx, cfg, acct, tc.st); got != tc.want {
				t.Errorf("判成 %v，应该是 %v——%s", got, tc.want, tc.why)
			}
		})
	}

	// 从没同步过的箱（没有 mail_sync_state 那一行）：空信箱不必同步，
	// 有信就必须同步。新绑的箱走的正是这条路。
	if _, err := pool.Exec(ctx, `DELETE FROM mail_sync_state WHERE tenant_id=$1`, tenantID); err != nil {
		t.Fatal(err)
	}
	if svc.worthAFullSync(ctx, cfg, acct, FolderStatus{UIDNext: 1, Unseen: 0}) {
		t.Error("一个空信箱被判成需要同步")
	}
	if !svc.worthAFullSync(ctx, cfg, acct, FolderStatus{UIDNext: 6, Unseen: 5}) {
		t.Error("新绑的箱里已经有五封信，却被判成不必同步——那五封永远不会进来")
	}
}

// 分档挑的是「有人在看的」，不是「所有活着的」。
//
// 挑错的后果分两边：全量那一档多挑了，等于没分档（600 个箱每轮全过一遍，
// 每个箱 1.6 秒）；挑少了，有人正开着邮箱页却掉进了十分钟一次的那一档。
func TestTheTwoTiersSplitByWhoIsLooking(t *testing.T) {
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
	employeeID := tenantID%100000 + 930001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	watching, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "watching@qq.com", Provider: "qq", Secret: "a",
	})
	if err != nil {
		t.Fatal(err)
	}
	idle, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "idle@163.com", Provider: "netease163", Secret: "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 一个刚被看过，一个两小时前看过。
	if _, err := pool.Exec(ctx, `UPDATE mail_accounts SET last_read_at = now()
		WHERE tenant_id=$1 AND id=$2`, tenantID, watching.AccountID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE mail_accounts SET last_read_at = now() - interval '2 hours'
		WHERE tenant_id=$1 AND id=$2`, tenantID, idle.AccountID); err != nil {
		t.Fatal(err)
	}

	const activeSeconds = int32(30 * 60)
	active, err := svc.q.ListActiveMailAccounts(ctx, store.ListActiveMailAccountsParams{
		TenantID: tenantID, ActiveSeconds: activeSeconds,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != watching.AccountID {
		t.Fatalf("全量这一档应该只有正在看的那个箱，拿到 %v", idsOf(active))
	}

	due, err := svc.q.ListMailboxesDueForStatus(ctx, store.ListMailboxesDueForStatusParams{
		TenantID: tenantID, ActiveSeconds: activeSeconds,
		StatusSeconds: int32(10 * 60), RowLimit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != idle.AccountID {
		t.Fatalf("轻状态这一档应该只有没人看的那个箱，拿到 %v", dueIDsOf(due))
	}

	// 两档互斥。重叠的话正在看的箱会被问一次白问的 STATUS，而那笔钱本该
	// 花在没人看的箱上。
	svc.markStatusChecked(ctx, tenantID, idle.AccountID)
	due, err = svc.q.ListMailboxesDueForStatus(ctx, store.ListMailboxesDueForStatusParams{
		TenantID: tenantID, ActiveSeconds: activeSeconds,
		StatusSeconds: int32(10 * 60), RowLimit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Errorf("刚问过的箱又被挑中了——status_checked_at 没起作用，"+
			"结果是每轮都问一遍，等于没有限频：%v", dueIDsOf(due))
	}

	// 常开连接跟着同一个信号走。
	for _, tc := range []struct {
		name string
		id   int64
		want bool
	}{
		{"正在看的箱要守着", watching.AccountID, true},
		{"没人看的箱不守", idle.AccountID, false},
	} {
		got, err := svc.q.MailboxIsBeingRead(ctx, store.MailboxIsBeingReadParams{
			TenantID: tenantID, ID: tc.id, ActiveSeconds: activeSeconds,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("%s：拿到 %v", tc.name, got)
		}
	}
}

func idsOf(rows []store.ListActiveMailAccountsRow) []int64 {
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func dueIDsOf(rows []store.ListMailboxesDueForStatusRow) []int64 {
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// 解绑 = 断连接，不是删邮件。
//
// 这一条钉的是解绑之后**哪些事必须停、哪些事必须照常**。分不清的两个方向
// 各有各的坏法：
//
//	停多了 —— 历史邮件跟着消失。一个业务员离职、或者不想再让 ERP 连自己的
//	          私人 Gmail，客户的往来记录不该跟着没，那是公司的业务记录。
//	停少了 —— 一个没有凭据的箱还留在同步名单和发件人候选里。同步那边每轮
//	          跑一次注定失败的登录，把「认证失败」写到那一行上（而人是主动
//	          解绑的，那句话毫无道理）；发信那边更糟，信卡在队列里发不出去。
func TestUnbindingKeepsTheMailAndStopsEverythingElse(t *testing.T) {
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
	employeeID := tenantID%100000 + 940001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	gone, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "leaving@qq.com", Provider: "qq", Secret: "code-qq",
	})
	if err != nil {
		t.Fatal(err)
	}
	kept, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "staying@163.com", Provider: "netease163", Secret: "code-163",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 先绑的是默认发件箱——下面要解的正是它，好证明默认会被扶到另一个箱上。
	if def, _ := svc.defaultAccountIDFor(ctx, tenantID, employeeID); def != gone.AccountID {
		t.Fatalf("默认该是先绑的那个，拿到 %d", def)
	}
	// 这个箱里已经收过一封信。解绑之后它必须还在。
	if _, err := pool.Exec(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, subject, body_text, received_at)
		VALUES ($1,$2,$3,'kept@x','thr-kept','INBOX',1,'buyer@overseas.com','去年的询价','hi',now())`,
		tenantID, gone.AccountID, employeeID); err != nil {
		t.Fatal(err)
	}
	// 让它落进「有人在看」那一档，好证明解绑之后它会掉出同步名单。
	if _, err := pool.Exec(ctx, `UPDATE mail_accounts SET last_read_at = now()
		WHERE tenant_id=$1`, tenantID); err != nil {
		t.Fatal(err)
	}
	// **两把钥匙都得放上去。**
	//
	// 这个箱是用授权码绑的，所以 Google 那一列本来就是空的——只测密码那一列
	// 的话，「Google 的 refresh token 清了没」根本没被验到，而那是一把能一直
	// 登进人家 Gmail 的钥匙。手动塞一个进去，让下面那条断言对两列都有牙。
	if _, err := pool.Exec(ctx, `UPDATE mail_accounts
		SET oauth_refresh_enc = $3 WHERE tenant_id=$1 AND id=$2`,
		tenantID, gone.AccountID, []byte("pretend-google-refresh-token")); err != nil {
		t.Fatal(err)
	}

	if err := svc.UnbindMailbox(ctx, tenantID, employeeID, gone.AccountID); err != nil {
		t.Fatalf("解绑：%v", err)
	}

	// ---- 必须照常的 ----

	// 历史邮件还在。
	var kept1 int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM email_inbound
		WHERE tenant_id=$1 AND account_id=$2`, tenantID, gone.AccountID).Scan(&kept1); err != nil {
		t.Fatal(err)
	}
	if kept1 != 1 {
		t.Errorf("解绑把历史邮件也带走了：还剩 %d 封。解绑断的是连接，不是记录", kept1)
	}
	// 左栏那一行还在，而且标着解绑时间。
	boxes, err := svc.ListMyMailboxes(ctx, tenantID, employeeID)
	if err != nil {
		t.Fatal(err)
	}
	var shown *MailAccountView
	for i := range boxes {
		if boxes[i].ID == gone.AccountID {
			shown = &boxes[i]
		}
	}
	if shown == nil {
		t.Fatal("解绑之后这个箱从列表里消失了——那样历史邮件就没有入口了")
	}
	if shown.UnboundAt == "" {
		t.Error("列表里没标出它已经解绑，界面分不出「还能收发」和「只能看」")
	}

	// ---- 必须停的 ----

	// 凭据清干净了，两列都要清——只清一列等于留了一把还能登上去的钥匙。
	var sec, oauth []byte
	if err := pool.QueryRow(ctx, `SELECT secret_enc, oauth_refresh_enc FROM mail_accounts
		WHERE tenant_id=$1 AND id=$2`, tenantID, gone.AccountID).Scan(&sec, &oauth); err != nil {
		t.Fatal(err)
	}
	if len(sec) > 0 || len(oauth) > 0 {
		t.Errorf("解绑之后还留着凭据（密码 %d 字节 / Google %d 字节）", len(sec), len(oauth))
	}
	// 不再是发件人候选。
	if _, err := svc.mailboxOfMine(ctx, tenantID, employeeID, gone.AccountID); err == nil {
		t.Error("解绑的箱还能被点名当发件人——信会卡在队列里发不出去")
	}
	// 默认发件箱扶到了还绑着的那个。
	def, err := svc.defaultAccountIDFor(ctx, tenantID, employeeID)
	if err != nil {
		t.Fatal(err)
	}
	if def != kept.AccountID {
		t.Errorf("默认发件箱是 %d，应该扶到还绑着的 %d——落在解绑的箱上就发不出信",
			def, kept.AccountID)
	}
	// 掉出同步名单和常开连接名单。
	active, err := svc.q.ListActiveMailAccounts(ctx, store.ListActiveMailAccountsParams{
		TenantID: tenantID, ActiveSeconds: 1800,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range active {
		if a.ID == gone.AccountID {
			t.Error("解绑的箱还在同步名单里——每轮跑一次注定失败的登录，" +
				"然后把「认证失败」写到那一行上")
		}
	}
	watching, err := svc.q.MailboxIsBeingRead(ctx, store.MailboxIsBeingReadParams{
		TenantID: tenantID, ID: gone.AccountID, ActiveSeconds: 1800,
	})
	if err != nil {
		t.Fatal(err)
	}
	if watching {
		t.Error("解绑的箱还占着一条常开连接")
	}

	// ---- 留痕 ----
	var action string
	if err := pool.QueryRow(ctx, `SELECT action FROM mail_binding_log
		WHERE tenant_id=$1 AND account_id=$2 ORDER BY id DESC LIMIT 1`,
		tenantID, gone.AccountID).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if action != "UNBIND" {
		t.Errorf("留痕里最后一条是 %q，应该是 UNBIND", action)
	}

	// ---- 别人的箱解不了，解过的再解一次也不成 ----
	other := tenantID%100000 + 940002
	stranger, err := svc.VerifyMailSecret(ctx, tenantID, other, BindRequest{
		Email: "someone@qq.com", Provider: "qq", Secret: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.UnbindMailbox(ctx, tenantID, employeeID, stranger.AccountID); err == nil {
		t.Error("解掉了同事的信箱")
	}
	if err := svc.UnbindMailbox(ctx, tenantID, employeeID, gone.AccountID); err == nil {
		t.Error("同一个箱解了两次都说成功——留痕里「什么时候解的」会被推到第二次")
	}
}

// 重新填一次授权码就是重新绑上。
//
// 漏掉这一步的症状最难查：页面说「绑好了」，绿勾也回来了，**但信永远不来**
// ——因为所有挑信箱的查询都还在按 unbound_at 跳过它。
func TestRebindingAnUnboundMailboxBringsItBack(t *testing.T) {
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
	employeeID := tenantID%100000 + 950001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	first, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.UnbindMailbox(ctx, tenantID, employeeID, first.AccountID); err != nil {
		t.Fatal(err)
	}

	// 同一个地址重新填一次授权码。冲突键是地址，所以这是**同一行**复活，
	// 不是新增——历史邮件挂在这个 id 上，新增一行会把它们孤儿化。
	again, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code-2",
	})
	if err != nil {
		t.Fatalf("重新绑：%v", err)
	}
	if again.AccountID != first.AccountID {
		t.Fatalf("重新绑出了一行新的（%d ≠ %d）——历史邮件还挂在旧的那一行上，"+
			"从此看不到了", again.AccountID, first.AccountID)
	}

	var unbound *time.Time
	var active bool
	if err := pool.QueryRow(ctx, `SELECT unbound_at, is_active FROM mail_accounts
		WHERE tenant_id=$1 AND id=$2`, tenantID, first.AccountID).Scan(&unbound, &active); err != nil {
		t.Fatal(err)
	}
	if unbound != nil || !active {
		t.Errorf("重新绑之后还留着解绑标记（unbound_at=%v, is_active=%v）——"+
			"页面会说绑好了，而所有挑信箱的查询都还在跳过它，于是信永远不来",
			unbound, active)
	}
	// 能发信了。
	if _, err := svc.mailboxOfMine(ctx, tenantID, employeeID, first.AccountID); err != nil {
		t.Errorf("重新绑之后还不能当发件人：%v", err)
	}
}
