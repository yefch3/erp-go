package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 员工邮箱监管（00068）。这条路能读别人的信，所以它的规矩比功能本身重要：
//
//   - **只读**。看一眼别人的收件箱，不能把那封信在员工自己那边变成已读，
//     更不能把 Seen 推到邮件服务器上——那是员工在 Foxmail、在手机上也会
//     看到的一次改动。
//   - **每看一次留一行痕**，列表和开信都算。
//   - 只认收件箱和已发送。
//
// 这三条各写一条用例：它们错了不会报错，只会安静地变成"老板看过之后员工的
// 未读少了几封"或者"有人读了别人的信而没有任何记录"。

type supervisionFixture struct {
	svc      *Service
	pool     *pgxpool.Pool
	tenantID int64
}

func newSupervisionFixture(t *testing.T) (*supervisionFixture, context.Context) {
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
		_, _ = pool.Exec(ctx, "DELETE FROM mail_supervision_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
		pool.Close()
	})
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	return &supervisionFixture{svc: svc, pool: pool, tenantID: tenantID}, ctx
}

// seedEmployeeMail 给某个员工造一个信箱和一封**未读**的收件。
func (f *supervisionFixture) seedEmployeeMail(t *testing.T, employeeID int64, subject string) int64 {
	t.Helper()
	ctx := context.Background()
	var accountID int64
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO mail_accounts (tenant_id, employee_id, email, username, auth_kind, domain, secret_enc,
			smtp_host, smtp_port, smtp_security, imap_host, imap_port, imap_security)
		VALUES ($1,$2,$3,$3,'PASSWORD','example.test',''::bytea,'smtp.example.test',465,'SSL','imap.example.test',993,'SSL')
		RETURNING id`, f.tenantID, employeeID, fmt.Sprintf("e%d@example.test", employeeID)).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	var mailID int64
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO email_inbound (tenant_id, owner_id, account_id, folder, message_id, from_email, from_name,
			to_email, subject, body_text, body_html, snippet, received_at, is_read, imap_uid)
		VALUES ($1,$2,$3,'INBOX',$4,'buyer@overseas.test','Buyer',$5,$6,'hello','<p>hello</p>','hello', now(), false, 1)
		RETURNING id`,
		f.tenantID, employeeID, accountID,
		fmt.Sprintf("<%d@example.test>", time.Now().UnixNano()),
		fmt.Sprintf("e%d@example.test", employeeID), subject).Scan(&mailID); err != nil {
		t.Fatal(err)
	}
	return mailID
}

func (f *supervisionFixture) isRead(t *testing.T, id int64) bool {
	t.Helper()
	var read bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT is_read FROM email_inbound WHERE tenant_id=$1 AND id=$2`, f.tenantID, id).Scan(&read); err != nil {
		t.Fatal(err)
	}
	return read
}

func (f *supervisionFixture) logCount(t *testing.T, action string) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM mail_supervision_log WHERE tenant_id=$1 AND action=$2`,
		f.tenantID, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSupervisionReadsWithoutMarkingAnythingRead(t *testing.T) {
	f, ctx := newSupervisionFixture(t)
	const boss, sales = int64(7001), int64(7002)
	mailID := f.seedEmployeeMail(t, sales, "客户询价 RFQ-99")
	who := SupervisionViewer{EmployeeID: boss, Name: "老板", ClientIP: "10.0.0.9"}

	if f.isRead(t, mailID) {
		t.Fatal("造出来的这封本该是未读的")
	}
	view, _, err := f.svc.SuperviseMail(ctx, f.tenantID, who, sales, "小王", mailID)
	if err != nil {
		t.Fatalf("监管读信失败：%v", err)
	}
	if view.Subject != "客户询价 RFQ-99" {
		t.Errorf("读到的主题不对：%q", view.Subject)
	}
	// 这条是整个功能的底线：老板看过，员工那边不能有任何变化。
	if f.isRead(t, mailID) {
		t.Error("监管读信把员工的信标成已读了——员工在 Foxmail 里也会看到这一下")
	}
	// 而且它照实说这封还没读，不是顺手报个 true。
	if view.IsRead {
		t.Error("这封信员工还没读，监管这边不该显示成已读")
	}
	if n := f.logCount(t, "OPEN"); n != 1 {
		t.Errorf("开信该留一行痕，实际 %d 行", n)
	}
}

func TestSupervisionLogsEveryLookAndNamesBothSides(t *testing.T) {
	f, ctx := newSupervisionFixture(t)
	const boss, sales = int64(7101), int64(7102)
	f.seedEmployeeMail(t, sales, "报价确认")
	who := SupervisionViewer{EmployeeID: boss, Name: "老板", ClientIP: "10.0.0.9"}

	if _, err := f.svc.SuperviseFolder(ctx, f.tenantID, who, sales, "小李", "inbox", "", "", 25); err != nil {
		t.Fatalf("翻收件箱失败：%v", err)
	}
	if _, err := f.svc.SuperviseFolder(ctx, f.tenantID, who, sales, "小李", "sent", "", "", 25); err != nil {
		t.Fatalf("翻已发送失败：%v", err)
	}
	if n := f.logCount(t, "LIST"); n != 2 {
		t.Errorf("翻了两次该留两行，实际 %d 行", n)
	}
	// 两头的名字都要有：这张表是在他们离职之后被问起的。
	var viewer, target, detail, ip string
	if err := f.pool.QueryRow(ctx, `
		SELECT viewer_name, target_name, detail, client_ip FROM mail_supervision_log
		WHERE tenant_id=$1 AND action='LIST' ORDER BY id DESC LIMIT 1`, f.tenantID).
		Scan(&viewer, &target, &detail, &ip); err != nil {
		t.Fatal(err)
	}
	if viewer != "老板" || target != "小李" || detail != "sent" || ip != "10.0.0.9" {
		t.Errorf("留痕内容不对：viewer=%q target=%q detail=%q ip=%q", viewer, target, detail, ip)
	}

	// 认不出是谁在看就不给看：一条没有主语的日志等于没有日志。
	if _, err := f.svc.SuperviseFolder(ctx, f.tenantID, SupervisionViewer{}, sales, "小李", "inbox", "", "", 25); err == nil {
		t.Error("看的人是谁都不知道，不该放行")
	}
}

func TestSupervisionOnlyOpensInboxAndSent(t *testing.T) {
	f, ctx := newSupervisionFixture(t)
	const boss, sales = int64(7201), int64(7202)
	f.seedEmployeeMail(t, sales, "随便")
	who := SupervisionViewer{EmployeeID: boss, Name: "老板"}

	for _, folder := range []string{"trash", "drafts", "junk", "F:客户"} {
		if _, err := f.svc.SuperviseFolder(ctx, f.tenantID, who, sales, "小张", folder, "", "", 25); apierr.CodeFromError(err) != "MAIL_SUPERVISION_FOLDER" {
			t.Errorf("%s 不在监管范围里，该拒：%v", folder, err)
		}
	}
	// 拒掉的那几次不该留痕：什么都没看，记一行"看过"是假的。
	if n := f.logCount(t, "LIST"); n != 0 {
		t.Errorf("被拒的查看不该留痕，实际 %d 行", n)
	}
	// 不给员工也拒。
	if _, err := f.svc.SuperviseFolder(ctx, f.tenantID, who, 0, "", "inbox", "", "", 25); apierr.CodeFromError(err) != "MAIL_SUPERVISION_TARGET" {
		t.Errorf("没说看谁，该拒：%v", err)
	}
}

// 树上只列有信箱的人：没绑过箱的人点进去是一片空。
func TestSupervisableEmployeesListsOnlyPeopleWithMailboxes(t *testing.T) {
	f, ctx := newSupervisionFixture(t)
	const sales = int64(7301)
	f.seedEmployeeMail(t, sales, "一封未读")

	rows, err := f.svc.SupervisableEmployees(ctx, f.tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].EmployeeID != sales {
		t.Fatalf("该只有一个有信箱的人：%+v", rows)
	}
	if rows[0].Mailboxes != 1 {
		t.Errorf("信箱数不对：%d", rows[0].Mailboxes)
	}
	if rows[0].Unread != 1 {
		t.Errorf("未读数不对：%d", rows[0].Unread)
	}
}
