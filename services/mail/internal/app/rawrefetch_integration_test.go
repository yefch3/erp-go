package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
)

// The raw-key collision left messages with no original (raw_key = ''). The
// refetch pass goes back to the mail host for them, finding each by
// Message-ID rather than by the stored UID — the stored UID belongs to
// whatever generation the folder was in when the row was written, and
// trusting a UID across generations is the mistake that caused the loss.
//
// What only a database can prove here:
//  1. an original the host still has comes back, keyed for the folder's
//     CURRENT generation and current UID, not the stored ones
//  2. a fetch that returns some other message is refused — the row stays
//     empty rather than adopting a stranger's mail, which is the exact
//     corruption this whole family of repairs exists to undo
//  3. what the host no longer has is left alone and reported, not retried
//     into an error loop
//  4. the query only offers rows that are actually recoverable: raw_key
//     empty AND a Message-ID to search by
//
// Run with: MAIL_TEST_DSN=postgres://erp_mail:...@127.0.0.1:5433/erp_mail

// refetchHost plays the mail host. The embedded interface makes any call
// outside the refetch path panic, which is the assertion that the pass
// touches nothing else.
type refetchHost struct {
	Mailbox
	sent     string
	junk     string
	validity uint32
	// folder → messageID (bare, as stored) → current UID
	found map[string]map[string]uint32
	// folder → messages the host would serve
	serves map[string][]RawMessage
}

func (h *refetchHost) SentFolder(ctx context.Context, acct MailAccount) (string, error) {
	return h.sent, nil
}

func (h *refetchHost) JunkFolder(ctx context.Context, acct MailAccount) (string, error) {
	return h.junk, nil
}

func (h *refetchHost) FindUIDsByMessageIDs(ctx context.Context, acct MailAccount, folder string, ids []string) (map[string]uint32, error) {
	out := map[string]uint32{}
	for _, id := range ids {
		if uid, ok := h.found[folder][id]; ok {
			out[id] = uid
		}
	}
	return out, nil
}

func (h *refetchHost) FetchByUIDs(ctx context.Context, acct MailAccount, folder string, uids []uint32) (FetchResult, error) {
	res := FetchResult{UIDValidity: h.validity}
	want := map[uint32]bool{}
	for _, u := range uids {
		want[u] = true
	}
	for _, m := range h.serves[folder] {
		if want[m.UID] {
			res.Messages = append(res.Messages, m)
		}
	}
	return res, nil
}

// recordingFiles keeps what was stored, so the test can assert both the key
// and that nothing beyond the recovered original was written.
type recordingFiles struct {
	Files
	puts map[string][]byte
}

func (f *recordingFiles) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.puts[key] = b
	return nil
}

func refetchRaw(messageID string) []byte {
	return []byte(fmt.Sprintf(
		"Message-ID: <%s>\r\nFrom: someone@example.com\r\nSubject: t\r\n\r\nbody of %s\r\n",
		messageID, messageID))
}

func TestRawOriginalRefetch(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)

	files := &recordingFiles{puts: map[string][]byte{}}
	svc := New(pool, Deps{Files: files}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	const accountID, ownerID = 4242, 7001
	rawA := refetchRaw("a@example.com")

	host := &refetchHost{
		sent:     "SENT-ON-HOST",
		junk:     "JUNK-ON-HOST",
		validity: 777,
		found: map[string]map[string]uint32{
			// Found under fresh UIDs on purpose: the rows below store 614/615,
			// and the pass must key by what the host says today.
			"SENT-ON-HOST": {"a@example.com": 101, "b@example.com": 102},
			"JUNK-ON-HOST": {},
		},
		serves: map[string][]RawMessage{
			"SENT-ON-HOST": {
				{UID: 101, Raw: rawA},
				// UID 102 answers with a different message than the search
				// promised — a renumbering mid-flight, or a host being
				// creative. The pass must refuse it.
				{UID: 102, Raw: refetchRaw("stranger@example.com")},
			},
		},
	}
	svc.UseMailbox(host)

	for _, r := range []struct {
		folder, mid, rawKey string
		uid                 int64
	}{
		{"SENT", "a@example.com", "", 614},          // recoverable
		{"SENT", "b@example.com", "", 615},          // host serves a stranger
		{"JUNK", "c@example.com", "", 8},            // purged from the host
		{"SENT", "d@example.com", "mail/keep", 616}, // has its original; not the query's business
		{"SENT", "", "", 617},                       // no Message-ID: nothing to search by
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO email_inbound (tenant_id, account_id, owner_id, folder, imap_uid, message_id, raw_key, subject)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'refetch test')`,
			tenantID, accountID, ownerID, r.folder, r.uid, r.mid, r.rawKey); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := svc.q.ListInboundMissingRaw(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("the query offered %d rows, want 3 (empty raw_key with a Message-ID)", len(rows))
	}

	acct := MailAccount{AccountID: accountID, EmployeeID: ownerID}
	recovered, gone, failed := svc.refetchForAccount(ctx, tenantID, acct, rows)
	if recovered != 1 || gone != 1 || failed != 1 {
		t.Fatalf("recovered/gone/failed = %d/%d/%d, want 1/1/1", recovered, gone, failed)
	}

	wantKey := rawKeyFor(tenantID, accountID, "SENT", 777, 101)
	var gotKey string
	var gotSize int64
	if err := pool.QueryRow(ctx, `
		SELECT raw_key, raw_size FROM email_inbound
		WHERE tenant_id = $1 AND message_id = 'a@example.com'`,
		tenantID).Scan(&gotKey, &gotSize); err != nil {
		t.Fatal(err)
	}
	if gotKey != wantKey {
		t.Errorf("recovered row keyed %q, want %q (current generation, current UID)", gotKey, wantKey)
	}
	if gotSize != int64(len(rawA)) {
		t.Errorf("raw_size = %d, want %d", gotSize, len(rawA))
	}

	if got := string(files.puts[wantKey]); got != string(rawA) {
		t.Errorf("stored object is not the fetched original")
	}
	if len(files.puts) != 1 {
		t.Errorf("%d objects written, want exactly 1: a refused message must not reach storage", len(files.puts))
	}

	// The stranger and the purged one: still empty, still honest about it.
	for _, mid := range []string{"b@example.com", "c@example.com"} {
		var key string
		if err := pool.QueryRow(ctx, `
			SELECT raw_key FROM email_inbound WHERE tenant_id = $1 AND message_id = $2`,
			tenantID, mid).Scan(&key); err != nil {
			t.Fatal(err)
		}
		if key != "" {
			t.Errorf("row %s adopted %q; it must stay empty", mid, key)
		}
	}

	// Untouched: the row that has its original.
	var keepKey string
	if err := pool.QueryRow(ctx, `
		SELECT raw_key FROM email_inbound WHERE tenant_id = $1 AND message_id = 'd@example.com'`,
		tenantID).Scan(&keepKey); err != nil {
		t.Fatal(err)
	}
	if keepKey != "mail/keep" {
		t.Errorf("a row that had its original was touched: %q", keepKey)
	}
}

// A second pass over the same data must find nothing to do: recovery marks
// the row recovered by filling raw_key, and the query stops offering it.
func TestRawOriginalRefetchIsIdempotent(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)

	files := &recordingFiles{puts: map[string][]byte{}}
	svc := New(pool, Deps{Files: files}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	host := &refetchHost{
		sent: "S", junk: "J", validity: 9,
		found:  map[string]map[string]uint32{"S": {"x@example.com": 5}},
		serves: map[string][]RawMessage{"S": {{UID: 5, Raw: refetchRaw("x@example.com")}}},
	}
	svc.UseMailbox(host)

	if _, err := pool.Exec(ctx, `
		INSERT INTO email_inbound (tenant_id, account_id, owner_id, folder, imap_uid, message_id, raw_key, subject)
		VALUES ($1, 1, 1, 'SENT', 3, 'x@example.com', '', 'idempotence')`, tenantID); err != nil {
		t.Fatal(err)
	}

	acct := MailAccount{AccountID: 1, EmployeeID: 1}
	rows, err := svc.q.ListInboundMissingRaw(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if r, _, _ := svc.refetchForAccount(ctx, tenantID, acct, rows); r != 1 {
		t.Fatalf("first pass recovered %d, want 1", r)
	}

	rows, err = svc.q.ListInboundMissingRaw(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("second pass was offered %d rows, want 0", len(rows))
	}
}
