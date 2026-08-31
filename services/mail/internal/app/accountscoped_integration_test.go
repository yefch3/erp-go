package app

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 凭据和收发服务器都按**信箱**取，不按人取。
//
// 这条是为「一个人绑多个信箱」铺路的第一步，而它要防的东西今天还看不见——
// mail_accounts 上的 UNIQUE (tenant_id, employee_id) 还在，一个人只有一行，
// 所以"按人查"和"按信箱查"给出同一个答案。约束一旦放开，两者就分道扬镳，
// 而按人查的那条路**不会报错**：那句 SQL 是 sqlc 的 :one，生成 QueryRow，
// pgx 读到第一行就返回、既不报「多行」也没有 ORDER BY。于是发信随机挑箱、
// 同步只同步被挑中的那个，另一个信箱一封信都收不到，日志里一个字都没有。
//
// 所以这条测试趁约束还在的时候，先把「按 id 取」这件事钉死。做法是让
// employee_id 和账号 id 故意错开：如果哪天有人把查询改回按 employee_id，
// 拿到的就是别人的信箱，或者什么都拿不到。
//
// 顺带钉住 00042 那次搬迁：收发服务器现在长在信箱行上，不再是"一家公司
// 一份"。那份租户级配置是跨服务商的真正拦路虎——一家 263 的公司里，
// 拿 Gmail 地址去绑只会拿着 Gmail 的账号去登 imap.263.net。
func TestCredentialsAreLookedUpByMailboxNotByPerson(t *testing.T) {
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

	// 要一把真钥匙：ForAccount 在没有密钥时会**先**返回 ErrNoKey，根本不查库，
	// 那样这条测试就问不到「查的是哪一行」这件事。
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}

	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_hosts WHERE tenant_id=$1", tenantID)
	}()

	// 两个人，各一个信箱，**各自的服务商不同**——这正是搬迁要解锁的形态。
	//
	// 密文的 AAD 绑定行 id（AccountAAD），所以只能先插入拿到 id 再回填，
	// 和生产代码 UpsertMailAccountShell → SetMailAccountSecret 同一个顺序。
	bind := func(employeeID int64, email, smtpHost, imapHost string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO mail_accounts
			(tenant_id, employee_id, email, secret_enc, is_active,
			 smtp_host, smtp_port, smtp_security, imap_host, imap_port, imap_security)
			VALUES ($1,$2,$3,''::bytea,TRUE,$4,465,'SSL',$5,993,'SSL')
			RETURNING id`, tenantID, employeeID, email, smtpHost, imapHost).Scan(&id); err != nil {
			t.Fatal(err)
		}
		blob, err := box.Seal([]byte("authcode-"+email), AccountAAD(tenantID, id))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE mail_accounts
			SET secret_enc=$3, key_version=$4 WHERE tenant_id=$1 AND id=$2`,
			tenantID, id, blob, box.Version()); err != nil {
			t.Fatal(err)
		}
		return id
	}
	// 员工号取大数，保证和 BIGSERIAL 的账号 id 不可能撞上——撞上了这条测试
	// 就证明不了「按 id 查」这件事。
	empA, empB := tenantID%100000+700001, tenantID%100000+700002
	acctA := bind(empA, "a@sunrise.com", "smtp.263.net", "imap.263.net")
	acctB := bind(empB, "b@gmail.com", "smtp.gmail.com", "imap.gmail.com")
	if acctA == empA || acctB == empB {
		t.Fatalf("账号 id 和员工号撞上了（%d/%d），这条测试证明不了任何事", acctA, acctB)
	}

	svc := New(pool, Deps{Secrets: box}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// 按信箱取：拿到的必须正是那个信箱，连它自己的服务器地址一起。
	for _, c := range []struct {
		accountID         int64
		email, smtp, imap string
		employeeID        int64
	}{
		{acctA, "a@sunrise.com", "smtp.263.net", "imap.263.net", empA},
		{acctB, "b@gmail.com", "smtp.gmail.com", "imap.gmail.com", empB},
	} {
		got, err := svc.ForAccount(ctx, tenantID, c.accountID)
		if err != nil {
			t.Fatalf("按信箱 %d 取不到账号：%v", c.accountID, err)
		}
		if got.Secret != "authcode-"+c.email {
			t.Errorf("信箱 %s 解出来的授权码是 %q——密文的 AAD 绑定的是行 id，"+
				"解错说明取的不是这一行", c.email, got.Secret)
		}
		if got.Email != c.email {
			t.Errorf("信箱 %d 应该是 %s，拿到 %s——查询大概又改回按人查了",
				c.accountID, c.email, got.Email)
		}
		if got.Host != c.smtp || got.IMAPHost != c.imap {
			t.Errorf("信箱 %s 的服务器应该是 %s/%s，拿到 %s/%s——"+
				"收发服务器必须长在信箱行上，不能再是一家公司一份",
				c.email, c.smtp, c.imap, got.Host, got.IMAPHost)
		}
		if got.EmployeeID != c.employeeID {
			t.Errorf("信箱 %s 的归属人应该是 %d，拿到 %d", c.email, c.employeeID, got.EmployeeID)
		}
	}

	// 反面：拿员工号当账号 id 去查，必须查不到。这一条是整条测试的牙——
	// 查询改回按 employee_id 的那一刻，它会通过，而上面那些也会通过。
	if got, err := svc.ForAccount(ctx, tenantID, empA); err == nil && got.Email != "" {
		t.Errorf("拿员工号 %d 当信箱 id 竟然查到了 %s——说明查的还是 employee_id",
			empA, got.Email)
	}
}
