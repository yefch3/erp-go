package app

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

type dailyReportScopeStub struct {
	t          *testing.T
	visibility Visibility
}

func (s dailyReportScopeStub) VisibleEmployees(_ context.Context, employeeID int64, module string) (Visibility, error) {
	s.t.Helper()
	if employeeID != 10 || module != DailyReportScope {
		s.t.Fatalf("unexpected report scope lookup: employee %d, module %s", employeeID, module)
	}
	return s.visibility, nil
}

func TestDailyMailReportCountsAndScope(t *testing.T) {
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
	tenant := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"email_inbound", "email_messages", "mail_sync_state", "mail_accounts"} {
			if _, err := pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id IN ($1,$2)", tenant, tenant+1); err != nil {
				t.Error(err)
			}
		}
	}()
	day := time.Date(2026, 11, 22, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	asOf := day.Add(12 * time.Hour)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	bind := func(tid, employeeID int64, email string) int64 {
		t.Helper()
		var id int64
		err := pool.QueryRow(ctx, `INSERT INTO mail_accounts(tenant_id,employee_id,email,secret_enc,created_at)
			VALUES ($1,$2,$3,''::bytea,$4) RETURNING id`, tid, employeeID, email, day.AddDate(0, 0, -90)).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	mailboxA := bind(tenant, 10, "a@example.invalid")
	mailboxB := bind(tenant, 20, "b@example.invalid")
	mailboxC := bind(tenant+1, 10, "a@example.invalid")
	exec(`INSERT INTO mail_sync_state(tenant_id,account_id,folder,last_synced_at) VALUES ($1,$2,'INBOX',$3)`, tenant, mailboxA, asOf)

	inbound := func(tid, id, owner, uid int64, msg, thread, sender string, at time.Time) {
		t.Helper()
		exec(`INSERT INTO email_inbound(tenant_id,account_id,owner_id,imap_uid,message_id,thread_key,from_email,sent_at,received_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tid, id, owner, uid, msg, thread, sender, at, asOf)
	}
	inbound(tenant, mailboxA, 10, 1, "cc", "cc-thread", "buyer@client.invalid", day)
	inbound(tenant, mailboxA, 10, 2, "cc", "cc-thread", "buyer@client.invalid", day)
	inbound(tenant, mailboxB, 20, 1, "cc", "cc-thread", "buyer@client.invalid", day)
	inbound(tenant, mailboxA, 10, 3, "old", "old-thread", "buyer@client.invalid", asOf.Add(-48*time.Hour))
	inbound(tenant, mailboxA, 10, 4, "notice", "notice-thread", "noreply@client.invalid", day)
	inbound(tenant, mailboxA, 10, 5, "future", "future-thread", "buyer@client.invalid", asOf)
	inbound(tenant, mailboxA, 10, 6, "ancient", "ancient-thread", "buyer@client.invalid", day.AddDate(0, 0, -30))
	inbound(tenant+1, mailboxC, 10, 1, "private", "private-thread", "buyer@client.invalid", day)
	exec(`INSERT INTO email_messages(tenant_id,message_key,sender_id,account_id,to_email,subject,body,status,sent_at,thread_key,in_reply_to)
		VALUES ($1,gen_random_uuid(),10,$2,'buyer@client.invalid','','','ACCEPTED',$3,'cc-thread','cc'),
		       ($1,gen_random_uuid(),10,$2,'buyer@client.invalid','','','DELIVERED',$4,'cc-thread','cc'),
		       ($1,gen_random_uuid(),10,$2,'buyer@client.invalid','','','FAILED',$4,'old-thread','old')`,
		tenant, mailboxA, day.Add(time.Hour), day.Add(2*time.Hour))
	exec(`INSERT INTO email_inbound(tenant_id,account_id,owner_id,folder,imap_uid,sent_message_id,message_id,thread_key,in_reply_to,sent_at)
		SELECT tenant_id,account_id,sender_id,'SENT',id,id,message_key::text,thread_key,in_reply_to,sent_at
		FROM email_messages WHERE tenant_id=$1 AND status='ACCEPTED'`, tenant)

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := New(pool, Deps{Scopes: dailyReportScopeStub{t: t, visibility: Visibility{All: true}}}, log)
	all, err := svc.DailyReport(ctx, tenant, 10, day, asOf, "")
	if err != nil {
		t.Fatal(err)
	}
	want := DailyMailCounts{Received: 2, Sent: 2, Replied: 1, Pending: 2, Overdue: 1}
	if all.Total.Received != want.Received || all.Total.Sent != want.Sent || all.Total.Replied != want.Replied ||
		all.Total.Pending != want.Pending || all.Total.Overdue != want.Overdue || len(all.Employees) != 2 ||
		all.Mailboxes != 2 || all.SyncedMailboxes != 1 || !all.Total.OldestPendingAt.Equal(asOf.Add(-48*time.Hour)) {
		t.Fatalf("unexpected team report: %+v", all)
	}

	svc = New(pool, Deps{Scopes: dailyReportScopeStub{t: t, visibility: Visibility{EmployeeIDs: []int64{10}}}}, log)
	scoped, err := svc.DailyReport(ctx, tenant, 10, day, asOf, "")
	if err != nil || scoped.Total.Pending != 1 || len(scoped.Employees) != 1 ||
		scoped.Employees[0].EmployeeID != 10 || scoped.Mailboxes != 1 {
		t.Fatalf("unexpected scoped report: %+v, %v", scoped, err)
	}

	earlier := day.AddDate(0, 0, -8)
	inbound(tenant, mailboxA, 10, 7, "cc", "cc-thread", "buyer@client.invalid", earlier)
	exec(`INSERT INTO email_messages(tenant_id,message_key,sender_id,account_id,to_email,subject,body,status,sent_at,thread_key,in_reply_to)
		VALUES ($1,gen_random_uuid(),10,$2,'buyer@client.invalid','','','ACCEPTED',$3,'cc-thread','cc')`,
		tenant, mailboxA, earlier.Add(time.Hour))
	monthStart := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, day.Location())
	month, err := svc.DailyReport(ctx, tenant, 10, monthStart, asOf, "week")
	if err != nil {
		t.Fatal(err)
	}
	if month.Total.Replied != 1 || month.Total.Sent != 3 || month.Total.Pending != 1 {
		t.Fatalf("unexpected monthly totals: %+v", month.Total)
	}
	if month.Trend["2026-11-09"].Replied != 1 || month.Trend["2026-11-16"].Replied != 1 {
		t.Fatalf("each week must count replies independently: %+v", month.Trend)
	}
}
