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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// flakyDB 让某一句 SQL 失败，其余照常——模拟一次连接抖动。
type flakyDB struct {
	inner  store.DBTX
	failOn string
}

func (f flakyDB) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	println("EXEC>>", strings.SplitN(strings.TrimSpace(sql), "\n", 2)[0])
	if f.failOn != "" && strings.Contains(sql, f.failOn) {
		return pgconn.CommandTag{}, errors.New("write tcp: connection reset by peer")
	}
	return f.inner.Exec(ctx, sql, args...)
}

func (f flakyDB) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return f.inner.Query(ctx, sql, args...)
}

func (f flakyDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return f.inner.QueryRow(ctx, sql, args...)
}

func TestTmpPartialBindingLeavesShell(t *testing.T) {
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
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
	}()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := New(pool, Deps{Secrets: box}, log)
	// SetMailAccountSecret 那一句抖一下，别的照常。
	svc.q = store.New(flakyDB{inner: pool, failOn: "SET secret_enc"})

	_, err = svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@sunrise.com", Provider: "p263", Secret: "code-263",
	})
	t.Logf("VerifyMailSecret 返回：%v", err)
	if err == nil {
		t.Fatal("expected the flaky write to surface")
	}

	var id int64
	var secLen int
	var isDefault, isActive bool
	var smtpHost, authKind string
	row := pool.QueryRow(ctx,
		`SELECT id, length(secret_enc), is_default, is_active, smtp_host, auth_kind
		   FROM mail_accounts WHERE tenant_id=$1`, tenantID)
	if err := row.Scan(&id, &secLen, &isDefault, &isActive, &smtpHost, &authKind); err != nil {
		t.Fatalf("留下的那一行读不到：%v", err)
	}
	t.Logf("库里留下的行：id=%d secret_enc长度=%d is_default=%v is_active=%v smtp_host=%q auth_kind=%q",
		id, secLen, isDefault, isActive, smtpHost, authKind)

	var logs int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM mail_binding_log WHERE tenant_id=$1`, tenantID).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	t.Logf("mail_binding_log 行数：%d", logs)

	clean := New(pool, Deps{Secrets: box}, log)
	_, ferr := clean.ForAccount(ctx, tenantID, id)
	t.Logf("ForAccount：%v", ferr)
	_, serr := clean.ForSender(ctx, tenantID, employeeID)
	t.Logf("ForSender：%v", serr)

	syncable, err := clean.q.ListSyncableMailAccounts(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ListSyncableMailAccounts 拿到 %d 个信箱", len(syncable))
}
