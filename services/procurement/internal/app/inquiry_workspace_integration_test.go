package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type d1Access struct{}

func (d1Access) VisibleEmployees(_ context.Context, id int64, _ string) (Visibility, error) {
	return Visibility{EmployeeIDs: []int64{id}}, nil
}
func (d1Access) HasPermission(_ context.Context, id int64, p string) (bool, error) {
	return (id == 101 || id == 102) && strings.HasPrefix(p, "sales:") || (id == 201 || id == 202) && strings.HasPrefix(p, "procurement:") || (id == 301 || id == 302) && strings.HasPrefix(p, "shipping:"), nil
}

type d1Documents struct{}

func (d1Documents) SetAvailability(context.Context, int64, bool) error { return nil }
func TestD1InquiryLifecycle(t *testing.T) {
	dsn := os.Getenv("D1_PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("run with an explicit D1 isolated DSN; not part of the default unit run")
	}
	if !(strings.Contains(dsn, "@localhost:25433/erp_procurement?") || (os.Getenv("D1_CONTAINER_NETWORK") == "erp-d1-20260906_isolated" && strings.Contains(dsn, "@postgres:5432/erp_procurement?"))) {
		t.Fatal("explicit isolated D1 DSN required")
	}
	ctx := context.Background()
	pool, e := pgdb.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	s := New(pool, Deps{Scopes: d1Access{}, InquiryDocuments: d1Documents{}})
	var current *InquiryView
	call := func(id int64, in InquiryCommand) *InquiryView {
		t.Helper()
		r, e := s.InquiryWorkspace(ctx, tenant, Operator{ID: id, Name: "test"}, in)
		if e != nil {
			t.Fatalf("%s/%s: %v", in.View, in.Action, e)
		}
		return r.Item
	}
	current = call(101, InquiryCommand{Action: "save", View: "SALES", Body: InquiryBody{Customer: "D1 customer", Products: []InquiryProduct{{Product: "Steel", Specification: "Q235", Quantity: "3", Unit: "MT"}}}})
	originalLine := current.Body.Products[0].ID
	current = call(101, InquiryCommand{Action: "save", View: "SALES", ID: current.ID, Revision: current.Revision, Body: current.Body})
	if current.Body.Products[0].ID != originalLine {
		t.Fatal("line identity changed")
	}
	if _, e = s.InquiryWorkspace(ctx, tenant, Operator{ID: 102}, InquiryCommand{Action: "get", View: "SALES", ID: current.ID}); e == nil {
		t.Fatal("other salesperson read allowed")
	}
	if _, e = s.InquiryWorkspace(ctx, tenant, Operator{ID: 201}, InquiryCommand{Action: "get", View: "PROCUREMENT", ID: current.ID}); e == nil {
		t.Fatal("unsubmitted inquiry leaked")
	}
	current = call(101, InquiryCommand{Action: "submit", View: "SALES", ID: current.ID, Revision: current.Revision})
	rev := current.Revision
	q := InquiryQuoteBody{Company: "Factory A", Currency: "USD", Prices: []InquiryPrice{{ProductID: originalLine, Price: "12.34"}}}
	buyer := call(201, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: current.ID, Revision: rev, Quote: q})
	if len(buyer.Quotes) != 1 {
		t.Fatal("saved quote missing")
	}
	quote := buyer.Quotes[0]
	sales := call(101, InquiryCommand{Action: "get", View: "QUOTATIONS", ID: current.ID})
	if len(sales.Quotes) != 0 {
		t.Fatal("saved quote leaked to sales")
	}
	if _, e = s.InquiryWorkspace(ctx, tenant, Operator{ID: 202}, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: current.ID, Revision: rev, QuoteID: quote.ID, QuoteVersion: quote.Version, Quote: q}); e == nil {
		t.Fatal("non-author edit allowed")
	}
	buyer = call(201, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: current.ID, Revision: rev, QuoteID: quote.ID, QuoteVersion: quote.Version, Quote: q, Submit: true})
	quote = buyer.Quotes[0]
	q.Prices[0].Price = "15.67"
	call(202, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: current.ID, Revision: rev, QuoteID: quote.ID, QuoteVersion: quote.Version, Quote: q})
	sales = call(101, InquiryCommand{Action: "get", View: "QUOTATIONS", ID: current.ID})
	if len(sales.Quotes) != 1 || sales.Quotes[0].Body.Prices[0].Price != "15.67" {
		t.Fatal("submitted edit not synchronized")
	}
	logistics := InquiryQuoteBody{Company: "Forwarder", Charges: []InquiryCharge{{Name: "Ocean", Amount: "10.05", Quantity: "3", Currency: "USD"}, {Name: "Port", Amount: "21.11", Quantity: "2", Currency: "CNY"}}}
	call(301, InquiryCommand{Action: "quote", View: "LOGISTICS", ID: current.ID, Revision: rev, Quote: logistics, Submit: true})
	sales = call(101, InquiryCommand{Action: "get", View: "QUOTATIONS", ID: current.ID})
	if len(sales.Quotes) != 2 || sales.Quotes[1].Body.Totals["USD"] != "30.15" || sales.Quotes[1].Body.Totals["CNY"] != "42.22" {
		t.Fatal("currency totals incorrect")
	}
	current = call(101, InquiryCommand{Action: "withdraw", View: "SALES", ID: current.ID, Revision: rev})
	if current.State != "WITHDRAWN" || len(current.Quotes) != 0 {
		t.Fatal("withdrawal left quotes")
	}
	current = call(101, InquiryCommand{Action: "submit", View: "SALES", ID: current.ID, Revision: current.Revision})
	if _, e = s.InquiryWorkspace(ctx, tenant, Operator{ID: 201}, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: current.ID, Revision: rev, Quote: q, Submit: true}); e == nil {
		t.Fatal("stale round accepted")
	}
	// Quote writers and withdrawal contend for one root row. A winning early
	// write must be removed; a late write must be rejected, including resubmit.
	raceRevision := current.Revision
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = s.InquiryWorkspace(ctx, tenant, Operator{ID: 201}, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: current.ID, Revision: raceRevision, Quote: q, Submit: true})
		}()
	}
	close(start)
	withdrawn := call(101, InquiryCommand{Action: "withdraw", View: "SALES", ID: current.ID, Revision: raceRevision})
	wg.Wait()
	if withdrawn.State != "WITHDRAWN" {
		t.Fatal("race withdrawal failed")
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM inquiry_quotes WHERE tenant_id=$1 AND case_id=$2`, tenant, inquiryID(current.ID)).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("race left %d quotes: %v", remaining, err)
	}
	for _, actor := range []int64{102, 999} {
		if _, err := s.InquiryWorkspace(ctx, tenant, Operator{ID: actor}, InquiryCommand{Action: "get", View: "SALES", ID: current.ID}); err == nil {
			t.Fatal("scope/permission bypass")
		}
	}
	if _, err := s.InquiryWorkspace(ctx, tenant+1, Operator{ID: 101}, InquiryCommand{Action: "get", View: "SALES", ID: current.ID}); err == nil {
		t.Fatal("tenant bypass")
	}
	t.Logf("isolated tenant=%d inquiry=%s: source identity, visibility, author/co-worker edit, exact multi-currency totals, withdrawal/resubmit passed; export dependency simulated", tenant, current.ID)
}
