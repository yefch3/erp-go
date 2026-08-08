package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The parts of the export that only a database can prove: that the record is
// written, that it describes the file that was actually handed over, and that
// a thread key belonging to somebody else opens nothing.
//
// Run with: MAIL_TEST_DSN=postgres://erp_mail:...@127.0.0.1:5433/erp_mail

func exportTestPool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("set MAIL_TEST_DSN to a migrated PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, ctx
}

// A tenant id far from any real one, unique per run, deleted afterwards.
func scratchTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	id := time.Now().UnixNano()/1000 + 940000000
	t.Cleanup(func() {
		for _, tbl := range []string{"mail_export_log", "email_inbound", "email_messages"} {
			if _, err := pool.Exec(ctx, "DELETE FROM "+tbl+" WHERE tenant_id = $1", id); err != nil {
				t.Errorf("cleanup %s: %v", tbl, err)
			}
		}
	})
	return id
}

func seedThread(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, ownerID int64, key string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO email_messages
		  (tenant_id, message_key, sender_id, sender_name, to_email, subject, body,
		   body_format, status, thread_key, sent_at)
		VALUES ($1, gen_random_uuid(), $2, '李娜', 'buyer@example.com',
		        '报价 SS304', '附上报价，请查收。', 'TEXT', 'ACCEPTED', $3,
		        '2026-03-01 08:00:00+00')`,
		tenantID, ownerID, key); err != nil {
		t.Fatalf("seed sent: %v", err)
	}
	uid := time.Now().UnixNano() % 1000000
	if _, err := pool.Exec(ctx, `
		INSERT INTO email_inbound
		  (tenant_id, account_id, owner_id, folder, imap_uid, message_id,
		   thread_key, from_email, from_name, subject, body_html, body_text, sent_at)
		VALUES ($1, 1, $2, 'INBOX', $3, $4, $5,
		        'buyer@example.com', 'Ana Costa', 'Re: 报价 SS304',
		        '<p>Please confirm 120 tons.</p>', 'Please confirm 120 tons.',
		        '2026-03-02 10:30:00+00')`,
		tenantID, ownerID, uid, fmt.Sprintf("reply-%d", uid), key); err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
}

func countExportRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID int64) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM mail_export_log WHERE tenant_id = $1", tenantID).Scan(&n); err != nil {
		t.Fatalf("count log: %v", err)
	}
	return n
}

// The whole of #96 in one assertion: the row exists, it names the person, and
// its byte count is the byte count of the file that was returned — not an
// estimate, not the size before rendering.
func TestAnExportLeavesARecordOfItself(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	const key = "thread-export-integration"
	seedThread(t, ctx, pool, tenantID, 5001, key)

	op := Operator{ID: 5001, Name: "李娜"}
	doc, err := svc.ExportMailThread(ctx, tenantID, op, ExportRequest{
		ThreadKey: key, Zone: "Asia/Shanghai", Lang: "zh", ClientIP: "203.0.113.9",
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if doc.TurnCount != 2 {
		t.Fatalf("turn count = %d, want both sides of the exchange", doc.TurnCount)
	}

	var (
		who, ip, subject string
		turns            int32
		size             int64
	)
	if err := pool.QueryRow(ctx, `
		SELECT employee_name, client_ip, subject, turn_count, byte_size
		FROM mail_export_log WHERE tenant_id = $1`, tenantID).
		Scan(&who, &ip, &subject, &turns, &size); err != nil {
		t.Fatalf("no record was written: %v", err)
	}
	if who != "李娜" || ip != "203.0.113.9" {
		t.Fatalf("record says %q from %q", who, ip)
	}
	if subject != "报价 SS304" {
		t.Fatalf("record subject = %q; the first subject, not the Re:", subject)
	}
	if turns != 2 || size != int64(len(doc.Content)) {
		t.Fatalf("record says %d turns / %d bytes; the file is %d bytes", turns, size, len(doc.Content))
	}
}

// Both halves of the conversation, in the order they happened, in the
// reader's own clock. 08:00 UTC is 16:00 in Shanghai, and a transcript that
// disagrees with the customer's memory of the day is worth nothing.
func TestTheTranscriptHoldsBothSidesInTheReadersClock(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	const key = "thread-export-order"
	seedThread(t, ctx, pool, tenantID, 5002, key)

	doc, err := svc.ExportMailThread(ctx, tenantID, Operator{ID: 5002, Name: "李娜"},
		ExportRequest{ThreadKey: key, Zone: "Asia/Shanghai", Lang: "zh"})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	body := string(doc.Content)
	sent := strings.Index(body, "附上报价，请查收。")
	reply := strings.Index(body, "Please confirm 120 tons.")
	if sent < 0 || reply < 0 {
		t.Fatalf("a side of the conversation is missing:\n%s", body)
	}
	if sent > reply {
		t.Fatal("the reply came before the mail it answered")
	}
	if !strings.Contains(body, "2026-03-01 16:00 +08:00") {
		t.Fatalf("the send is not stamped in the reader's zone:\n%s", body)
	}
}

// A thread key is a guessable string. Somebody else's conversation must open
// nothing — and, just as importantly, the attempt must not write a log entry
// saying they exported it, because a log full of things that did not happen
// is a log nobody can act on.
func TestSomebodyElsesConversationExportsNothingAndLogsNothing(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	const key = "thread-export-not-yours"
	seedThread(t, ctx, pool, tenantID, 5003, key)

	_, err := svc.ExportMailThread(ctx, tenantID, Operator{ID: 9999, Name: "Somebody Else"},
		ExportRequest{ThreadKey: key})
	if err == nil {
		t.Fatal("another employee's conversation was exported")
	}
	if n := countExportRows(t, ctx, pool, tenantID); n != 0 {
		t.Fatalf("%d records were written for an export that was refused", n)
	}
}

func TestAnEmptyThreadKeyIsRefusedBeforeAnythingIsRead(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if _, err := svc.ExportMailThread(ctx, tenantID, Operator{ID: 5004},
		ExportRequest{ThreadKey: "  "}); err == nil {
		t.Fatal("an empty thread key produced a document")
	}
	if n := countExportRows(t, ctx, pool, tenantID); n != 0 {
		t.Fatalf("%d records were written for a request that never ran", n)
	}
}
