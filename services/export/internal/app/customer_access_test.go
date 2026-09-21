package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"os"
	"testing"
	"time"
)

type customerAccessFixture struct {
	ids []int64
	all bool
}

func (f *customerAccessFixture) VisibleIDs(context.Context, int64) ([]int64, bool, error) {
	return f.ids, f.all, nil
}
func (f *customerAccessFixture) Get(_ context.Context, id int64) (Customer, error) {
	for _, v := range f.ids {
		if id == v {
			return Customer{ID: id, Name: "Canonical", Status: "ACTIVE"}, nil
		}
	}
	return Customer{}, apierr.Permission("CUSTOMER_DENIED", "denied")
}
func TestOfferCannotForgeCustomer(t *testing.T) {
	s := &Service{customers: &customerAccessFixture{ids: []int64{10}}}
	for _, id := range []string{"20", "0", ""} {
		if err := s.validateOfferCustomer(context.Background(), &OfferBody{CustomerID: id, Customer: "Forged"}); err == nil {
			t.Fatalf("accepted customer %q", id)
		}
	}
	b := OfferBody{CustomerID: "10", Customer: "Forged"}
	if err := s.validateOfferCustomer(context.Background(), &b); err != nil || b.Customer != "Canonical" {
		t.Fatalf("valid customer: %+v %v", b, err)
	}
}
func TestCustomerBusinessListsIntegration(t *testing.T) {
	dsn := os.Getenv("CUSTOMER_EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("CUSTOMER_EXPORT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano() / 1000
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM contracts WHERE tenant_id=$1", tenant)
		_, _ = pool.Exec(ctx, "DELETE FROM quotations WHERE tenant_id=$1", tenant)
	}()
	customers := &customerAccessFixture{ids: []int64{10}}
	s := New(pool, Deps{Customers: customers})
	for _, cid := range []int64{10, 20} {
		_, err = pool.Exec(ctx, `INSERT INTO quotations(tenant_id,quote_no,customer_id,customer_name,currency,fx_rate,fx_rate_at,fx_source,sales_employee_id) VALUES($1,'Q-'||($2::bigint)::text,$2,'Snapshot','USD',1,now(),'test',1)`, tenant, cid)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO contracts(tenant_id,contract_no,customer_id,customer_name,sales_employee_id) VALUES($1,'C-'||($2::bigint)::text,$2,'Snapshot',1)`, tenant, cid)
		if err != nil {
			t.Fatal(err)
		}
	}
	assertLists := func(want int64) {
		t.Helper()
		quotes, total, err := s.ListQuotations(ctx, tenant, QuotationFilter{}, 1, 1, Operator{ID: 1})
		if err != nil || total != want || int64(len(quotes)) != min(want, 1) {
			t.Fatalf("quotes len=%d total=%d err=%v", len(quotes), total, err)
		}
		contracts, total, err := s.ListContracts(ctx, tenant, "", 0, "", 1, 1, Operator{ID: 1})
		if err != nil || total != want || int64(len(contracts)) != min(want, 1) {
			t.Fatalf("contracts len=%d total=%d err=%v", len(contracts), total, err)
		}
		if want == 1 && (quotes[0].CustomerID != 10 || contracts[0].CustomerID != 10) {
			t.Fatal("filtered after pagination or wrong customer")
		}
	}
	assertLists(1)
	customers.ids = nil
	assertLists(0)
	customers.ids = []int64{10}
	assertLists(1)
	customers.all = true
	assertLists(2)
	other, _, err := s.ListQuotations(ctx, tenant+1, QuotationFilter{}, 1, 20, Operator{ID: 1})
	if err != nil || len(other) != 0 {
		t.Fatal("admin list crossed tenant")
	}
	customers.all = false
	quote, err := pool.Query(ctx, `SELECT id FROM quotations WHERE tenant_id=$1 AND customer_id=20`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	quote.Next()
	_ = quote.Scan(&id)
	quote.Close()
	if _, _, err := s.GetQuotationFor(ctx, tenant, id, Operator{ID: 1}); err == nil {
		t.Fatal("direct quotation access leaked")
	}
	// Customer changes affect access, not recorded business snapshots.
	var name string
	if err := pool.QueryRow(ctx, "SELECT customer_name FROM quotations WHERE tenant_id=$1 AND id=$2", tenant, id).Scan(&name); err != nil || name != "Snapshot" {
		t.Fatal("historical snapshot changed")
	}
	_, err = pool.Exec(ctx, `INSERT INTO customer_offers(tenant_id,case_id,body,source_snapshot,updated_by) VALUES($1,123,'{"customerId":"20"}','{}',1)`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, "DELETE FROM customer_offers WHERE tenant_id=$1", tenant); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	summaries, err := s.offerSummaries(ctx, grpcx.Operator{TenantID: tenant, EmployeeID: 1})
	if err != nil || summaries != "{\"summaries\":{}}" {
		t.Fatalf("summary leak: %s %v", summaries, err)
	}
}
