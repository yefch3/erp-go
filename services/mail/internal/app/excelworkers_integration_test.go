package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type failingExtractor struct{ err error }

func (e failingExtractor) Extract(context.Context, TableExtractionInput) (Extraction, error) {
	return Extraction{}, e.err
}

// key 被拒、余额用完：人看到的是一句说得清的话，运维收到专门的告警，而不是和
// 「表格读不懂」一样的「智能转换失败，请稍后重试」。
func TestAModelAccountProblemIsToldApartFromAnOrdinaryFailure(t *testing.T) {
	for _, c := range []struct {
		name  string
		err   error
		code  string
		event string
	}{
		{"account", fmt.Errorf("%w: OpenAI returned HTTP 401: bad key", ErrModelAccount), "MAIL_EXCEL_MODEL_ACCOUNT", excelEventModelAccount},
		{"busy", fmt.Errorf("%w: OpenAI returned HTTP 429: slow down", ErrModelBusy), "MAIL_EXCEL_MODEL_BUSY", excelEventModelBusy},
		{"ordinary", errors.New("decode inquiry JSON: unexpected end"), "MAIL_EXCEL_MODEL_FAILED", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newExcelFixture(t)
			f.svc.tables = failingExtractor{err: c.err}
			row := f.claimed(t)
			f.svc.processExcelJob(context.Background(), row)
			if got := f.state(t, row.ID); got.status != "FAILED" || got.code != c.code {
				t.Errorf("job ended %s/%s, want FAILED/%s", got.status, got.code, c.code)
			}
			for _, e := range []string{excelEventModelAccount, excelEventModelBusy} {
				if f.loggedEvent(e) != (e == c.event) {
					t.Errorf("event %s logged = %v, want %v", e, f.loggedEvent(e), e == c.event)
				}
			}
		})
	}
}

// gateExtractor 让带着 marker 的转换停在半路，记下同时有几个在跑。别的（库里
// 可能有别的测试留下的任务）立刻返回，不算数。
type gateExtractor struct {
	marker        string
	release       chan struct{}
	mu            sync.Mutex
	running, peak int
}

func (g *gateExtractor) Extract(ctx context.Context, in TableExtractionInput) (Extraction, error) {
	if !strings.Contains(in.Text, g.marker) {
		return Extraction{}, errors.New("not this test's job")
	}
	g.mu.Lock()
	g.running++
	g.peak = max(g.peak, g.running)
	g.mu.Unlock()
	select {
	case <-g.release:
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
	}
	g.mu.Lock()
	g.running--
	g.mu.Unlock()
	return Extraction{}, errors.New("released")
}

func (g *gateExtractor) peakNow() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.peak
}

// 几个 worker 同时领、同时转。原来只有一个，全公司排一条队。
func TestSeveralConversionsRunAtTheSameTime(t *testing.T) {
	for _, workers := range []int{1, 3} {
		t.Run(fmt.Sprintf("%d workers", workers), func(t *testing.T) {
			f := newExcelFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			marker := fmt.Sprintf("pool-%d-%d", f.tid, workers)
			var mail int64
			if err := f.pool.QueryRow(ctx, `INSERT INTO email_inbound
				(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
				 from_email, to_email, subject, body_text, body_html, received_at)
				VALUES ($1, 1, $2, $3::text, $3::text, 'INBOX', 4343, 'c@x', 'me@co.com', 's', $3::text, '', now())
				RETURNING id`, f.tid, excelTestOwner, marker).Scan(&mail); err != nil {
				t.Fatal(err)
			}
			for range 4 {
				if _, err := f.pool.Exec(ctx, `INSERT INTO mail_excel_jobs
					(tenant_id, owner_id, inbound_id, selected_text, status) VALUES ($1, $2, $3, $4, 'PENDING')`,
					f.tid, excelTestOwner, mail, marker); err != nil {
					t.Fatal(err)
				}
			}
			gate := &gateExtractor{marker: marker, release: make(chan struct{})}
			f.svc.tables = gate
			done := make(chan struct{})
			go func() { f.svc.RunExcelWorkers(ctx, workers); close(done) }()

			deadline := time.Now().Add(5 * time.Second)
			for gate.peakNow() < workers && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			// 再多给一点时间：只开 1 个时要确认它**不会**冒出第 2 个。
			time.Sleep(300 * time.Millisecond)
			if got := gate.peakNow(); got != workers {
				t.Errorf("%d conversions ran at once, want %d", got, workers)
			}
			close(gate.release)
			cancel()
			<-done
		})
	}
}
