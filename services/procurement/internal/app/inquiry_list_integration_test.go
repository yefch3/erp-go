package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// listAccess counts the IAM calls a list makes. 101 and 102 are salespeople
// who see their own inquiries, 103 a read-only sales manager who sees every
// one, 201 a buyer, 301 a forwarder.
type listAccess struct{ visible, permission int }

func (a *listAccess) VisibleEmployees(_ context.Context, id int64, _ string) (Visibility, error) {
	a.visible++
	return Visibility{All: id == 103, EmployeeIDs: []int64{id}}, nil
}
func (a *listAccess) HasPermission(_ context.Context, id int64, p string) (bool, error) {
	a.permission++
	switch {
	case id == 101 || id == 102:
		return strings.HasPrefix(p, "sales:"), nil
	case id == 103:
		return p == "sales:inquiry:read", nil
	case id == 201:
		return strings.HasPrefix(p, "procurement:"), nil
	case id == 301:
		return strings.HasPrefix(p, "shipping:"), nil
	}
	return false, nil
}

// listCustomers is masterdata as the list sees it: 10 and 11 are active, and
// a legacy row that saved only "Canonical" resolves to 10. names is what the
// customers are called now, which a test can change after the inquiries were
// saved under the old names.
type listCustomers struct {
	names                      map[int64]string
	inactive                   map[int64]bool
	check, visibleIDs, resolve int
}

func newListCustomers() listCustomers {
	return listCustomers{names: map[int64]string{10: "Canonical", 11: "Second"}, inactive: map[int64]bool{}}
}

func (c *listCustomers) Check(_ context.Context, id int64) (string, error) {
	c.check++
	if c.inactive[id] {
		return "", apierr.Invalid("MD_CUSTOMER_INACTIVE", "客户已停用")
	}
	if name, ok := c.names[id]; ok {
		return name, nil
	}
	return "", apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "客户不存在或无权访问")
}
func (c *listCustomers) VisibleIDs(context.Context, int64) ([]int64, bool, error) {
	c.visibleIDs++
	return []int64{10, 11}, false, nil
}
func (c *listCustomers) ResolveByName(_ context.Context, name string) (int64, string, error) {
	c.resolve++
	if name == "Canonical" {
		return 10, "Canonical", nil
	}
	return 0, "", nil
}

// listCustomersBatch adds the one-call batch production uses.
type listCustomersBatch struct {
	listCustomers
	batch int
}

func (c *listCustomersBatch) CurrentNames(_ context.Context, ids []int64) (map[int64]CustomerCurrentName, error) {
	c.batch++
	out := map[int64]CustomerCurrentName{}
	for _, id := range ids {
		if name, ok := c.names[id]; ok {
			out[id] = CustomerCurrentName{Name: name, Active: !c.inactive[id]}
		}
	}
	return out, nil
}

// legacyListInquiryWorkspace is the list as it was before 2026-10: every
// matching inquiry read in full through readInquiry, then filtered and paged.
// It stays here as the oracle the new list must agree with.
func (s *Service) legacyListInquiryWorkspace(ctx context.Context, tenant int64, op Operator, in InquiryCommand) (InquiryResult, error) {
	visible := Visibility{All: true}
	var err error
	if in.View == "SALES" || in.View == "QUOTATIONS" {
		visible, err = s.visibleSourcingTo(ctx, op)
		if err != nil {
			return InquiryResult{}, err
		}
	}
	customerAll := true
	var customerIDs []int64
	if (in.View == "SALES" || in.View == "QUOTATIONS") && s.customers != nil {
		customerIDs, customerAll, err = s.customers.VisibleIDs(ctx, op.ID)
		if err != nil {
			return InquiryResult{}, err
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM sourcing_cases WHERE tenant_id=$1 AND ($6 OR customer_id=0 OR customer_id=ANY($7::bigint[])) AND status<>'CANCELLED' AND deleted_at IS NULL AND ($5 OR (status<>'INTAKE_PENDING' AND handoff_status<>'SALES_WITHDRAWN')) AND ($2 OR owner_id=ANY($3::bigint[])) AND ($4='' OR case_no ILIKE '%'||$4||'%' OR display_inquiry_no ILIKE '%'||$4||'%' OR customer_name ILIKE '%'||$4||'%' OR inquiry_body::text ILIKE '%'||$4||'%') ORDER BY updated_at DESC,id DESC`, tenant, visible.All, visible.EmployeeIDs, strings.TrimSpace(in.Keyword), in.View == "SALES", customerAll, customerIDs)
	if err != nil {
		return InquiryResult{}, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return InquiryResult{}, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return InquiryResult{}, err
	}
	out := InquiryResult{Items: []InquiryView{}}
	if in.Page < 1 {
		in.Page = 1
	}
	if in.Size != 20 && in.Size != 50 && in.Size != 100 {
		in.Size = 20
	}
	for _, id := range ids {
		v, e := s.readInquiry(ctx, tenant, id, op, in.View)
		if e != nil {
			if in.View == "PROCUREMENT" || in.View == "LOGISTICS" {
				var state string
				_ = s.pool.QueryRow(ctx, `SELECT status FROM sourcing_cases WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&state)
				if state == "INTAKE_PENDING" {
					continue
				}
			}
			return InquiryResult{}, e
		}
		if in.View == "QUOTATIONS" && v.State != "INQUIRING" {
			continue
		}
		state := v.State
		if in.View == "PROCUREMENT" {
			state = "WAITING"
			if v.ProcurementCount > 0 {
				state = "QUOTED"
			}
		}
		if in.View == "LOGISTICS" {
			state = "WAITING"
			if v.LogisticsCount > 0 {
				state = "QUOTED"
			}
		}
		if in.State != "" && in.State != state {
			continue
		}
		out.Total++
		if out.Total > (in.Page-1)*in.Size && len(out.Items) < in.Size {
			v.Quotes = nil
			out.Items = append(out.Items, *v)
		}
	}
	return out, nil
}

func listTestService(t *testing.T, customers CustomerAccess) (*Service, *listAccess, int64) {
	t.Helper()
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	pool, err := pgdb.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	access := &listAccess{}
	return New(pool, Deps{Scopes: access, Customers: customers, InquiryDocuments: d1Documents{}}), access, time.Now().UnixNano() / 1000
}

// seedInquiries builds every state the list distinguishes and returns the
// ids it made, newest last.
func seedInquiries(t *testing.T, s *Service, tenant int64, extra int) []string {
	t.Helper()
	ctx := context.Background()
	call := func(op int64, in InquiryCommand) *InquiryView {
		t.Helper()
		r, err := s.InquiryWorkspace(ctx, tenant, Operator{ID: op, Name: fmt.Sprintf("op%d", op)}, in)
		if err != nil {
			t.Fatalf("%d %s/%s: %v", op, in.View, in.Action, err)
		}
		return r.Item
	}
	body := func(customer string, title string) InquiryBody {
		return InquiryBody{Title: title, CustomerID: customer, Customer: "snapshot " + customer, Products: []InquiryProduct{{Product: "Steel " + title, Specification: "Q235", Quantity: "3", Unit: "MT"}}}
	}
	submit := func(op int64, customer, title string) *InquiryView {
		v := call(op, InquiryCommand{Action: "save", View: "SALES", Body: body(customer, title)})
		return call(op, InquiryCommand{Action: "submit", View: "SALES", ID: v.ID, Revision: v.Revision})
	}
	ids := []string{}
	unsubmitted := call(101, InquiryCommand{Action: "save", View: "SALES", Body: body("10", "unsubmitted")})
	ids = append(ids, unsubmitted.ID)

	quoted := submit(101, "11", "procurement quoted")
	price := InquiryQuoteBody{Company: "Factory", Currency: "USD", QuoteCategory: "FOB_USD", Incoterm: "FOB", Prices: []InquiryPrice{{ProductID: quoted.Body.Products[0].ID, Price: "12.34"}}}
	call(201, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: quoted.ID, Revision: quoted.Revision, Quote: price, Submit: true})
	ids = append(ids, quoted.ID)

	shipped := submit(101, "10", "logistics quoted")
	freight := InquiryQuoteBody{Company: "Forwarder", Incoterm: "CFR", ExchangeRates: map[string]string{"CNY": "7"}, Charges: []InquiryCharge{{Name: "Ocean", Amount: "10", Quantity: "1", Currency: "USD"}}}
	call(301, InquiryCommand{Action: "quote", View: "LOGISTICS", ID: shipped.ID, Revision: shipped.Revision, Quote: freight, Submit: true})
	draft := price
	draft.Prices = []InquiryPrice{{ProductID: shipped.Body.Products[0].ID, Price: "9"}}
	call(201, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: shipped.ID, Revision: shipped.Revision, Quote: draft})
	ids = append(ids, shipped.ID)

	withdrawn := submit(101, "11", "withdrawn")
	call(101, InquiryCommand{Action: "withdraw", View: "SALES", ID: withdrawn.ID, Revision: withdrawn.Revision})
	ids = append(ids, withdrawn.ID)

	named := submit(101, "11", "name only")
	if _, err := s.pool.Exec(ctx, `UPDATE sourcing_cases SET customer_id=0,customer_name='Canonical' WHERE tenant_id=$1 AND id=$2`, tenant, inquiryID(named.ID)); err != nil {
		t.Fatal(err)
	}
	ids = append(ids, named.ID)

	legacy, err := s.CreateSourcingCase(ctx, tenant, NewSourcingCase{CustomerID: 10, CustomerName: "Canonical", Lines: []SourcingLineInput{{Product: "Legacy coil", Quantity: "2", QuantityUnit: "MT"}}}, Operator{ID: 101, Name: "op101"})
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, strconv.FormatInt(legacy.Head.ID, 10))

	other := submit(102, "10", "another salesperson")
	ids = append(ids, other.ID)
	for i := 0; i < extra; i++ {
		ids = append(ids, submit(101, []string{"10", "11"}[i%2], fmt.Sprintf("bulk %02d", i)).ID)
	}
	return ids
}

func TestInquiryListMatchesReadingEveryInquiry(t *testing.T) {
	plain := newListCustomers()
	batch := &listCustomersBatch{listCustomers: newListCustomers()}
	// The legacy list reads every row, so it links the name-only inquiry on
	// its first call; TestInquiryListLinksNameOnlyCustomers covers the new
	// list doing that link itself.
	for _, tc := range []struct {
		name      string
		customers CustomerAccess
		names     map[int64]string
		inactive  map[int64]bool
	}{{"batch customer names", batch, batch.names, batch.inactive}, {"one Check per customer", &plain, plain.names, plain.inactive}} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, tenant := listTestService(t, tc.customers)
			seedInquiries(t, s, tenant, 23)
			// Renamed in masterdata after the inquiries were saved: lists show
			// the name the customer goes by now, not the saved snapshot.
			tc.names[10] = "Canonical Renamed"
			ctx := context.Background()
			views := map[int64][]string{101: {"SALES", "QUOTATIONS"}, 103: {"SALES", "QUOTATIONS"}, 201: {"PROCUREMENT"}, 301: {"LOGISTICS"}}
			states := map[string][]string{"SALES": {"", "UNSUBMITTED", "INQUIRING", "WITHDRAWN"}, "QUOTATIONS": {"", "INQUIRING"}, "PROCUREMENT": {"", "WAITING", "QUOTED"}, "LOGISTICS": {"", "WAITING", "QUOTED"}}
			checked := 0
			for op, vs := range views {
				for _, view := range vs {
					for _, state := range states[view] {
						for _, size := range []int{20, 100} {
							for _, page := range []int{1, 2} {
								for _, keyword := range []string{"", "bulk 1", "Canonical"} {
									in := InquiryCommand{Action: "list", View: view, State: state, Page: page, Size: size, Keyword: keyword}
									operator := Operator{ID: op, Name: fmt.Sprintf("op%d", op)}
									want, wantErr := s.legacyListInquiryWorkspace(ctx, tenant, operator, in)
									got, gotErr := s.listInquiryWorkspace(ctx, tenant, operator, in)
									if (wantErr == nil) != (gotErr == nil) {
										t.Fatalf("op %d %+v: errors differ: legacy %v, new %v", op, in, wantErr, gotErr)
									}
									a, _ := json.Marshal(want)
									b, _ := json.Marshal(got)
									if string(a) != string(b) {
										t.Fatalf("op %d %+v:\nlegacy %s\nnew    %s", op, in, a, b)
									}
									checked++
								}
							}
						}
					}
				}
			}
			if checked < 100 {
				t.Fatalf("only %d comparisons ran", checked)
			}
			// A deactivated customer on the page fails the list, as it always did.
			tc.inactive[11] = true
			defer delete(tc.inactive, 11)
			in := InquiryCommand{Action: "list", View: "SALES", Page: 1, Size: 100}
			if _, err := s.legacyListInquiryWorkspace(ctx, tenant, Operator{ID: 101}, in); err == nil {
				t.Fatal("legacy list accepted an inactive customer")
			}
			if _, err := s.listInquiryWorkspace(ctx, tenant, Operator{ID: 101}, in); err == nil {
				t.Fatal("new list accepted an inactive customer the old one refused")
			}
		})
	}
}

func TestInquiryListCostDoesNotGrowWithInquiries(t *testing.T) {
	customers := &listCustomersBatch{listCustomers: newListCustomers()}
	s, access, tenant := listTestService(t, customers)
	seedInquiries(t, s, tenant, 40)
	ctx := context.Background()
	*access, customers.batch = listAccess{}, 0
	customers.check, customers.visibleIDs, customers.resolve = 0, 0, 0
	r, err := s.listInquiryWorkspace(ctx, tenant, Operator{ID: 101, Name: "op101"}, InquiryCommand{Action: "list", View: "SALES", Page: 1, Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	if r.Total < 45 || len(r.Items) != r.Total {
		t.Fatalf("expected every seeded inquiry on one page, got %d of %d", len(r.Items), r.Total)
	}
	// Once each for the whole list: owner range, customer range, edit, delete.
	if access.visible != 1 || access.permission != 2 || customers.visibleIDs != 1 || customers.batch != 1 || customers.check != 0 {
		t.Fatalf("calls grew with the list: visible=%d permission=%d customerRange=%d batch=%d check=%d",
			access.visible, access.permission, customers.visibleIDs, customers.batch, customers.check)
	}
}

func TestInquiryWritesNudgeOpenPages(t *testing.T) {
	addr := os.Getenv("PROCUREMENT_TEST_REDIS")
	if addr == "" {
		t.Skip("PROCUREMENT_TEST_REDIS not set")
	}
	customers := newListCustomers()
	s, _, tenant := listTestService(t, &customers)
	publisher := livefeed.NewPublisher(addr, slog.Default())
	t.Cleanup(func() { _ = publisher.Close() })
	s.live = publisher
	subscriber := livefeed.NewSubscriber(addr)
	t.Cleanup(func() { _ = subscriber.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A buyer's page hears it too: the hint goes to the whole tenant.
	events := subscriber.Listen(ctx, tenant, 201)
	time.Sleep(200 * time.Millisecond) // let the subscription land before publishing

	// Other hints ride the same channel (an inquiry write also moves
	// purchasing's requirement list); only the inquiry one is under test.
	nextInquiryHint := func(wait time.Duration) (livefeed.Event, bool) {
		deadline := time.After(wait)
		for {
			select {
			case e := <-events:
				if e.Type == livefeed.InquiryChanged {
					return e, true
				}
			case <-deadline:
				return livefeed.Event{}, false
			}
		}
	}
	expectHint := func(what string) {
		t.Helper()
		e, ok := nextInquiryHint(3 * time.Second)
		if !ok {
			t.Fatalf("%s did not tell open pages", what)
		}
		if e.Subject != "" {
			t.Fatalf("%s: the tenant-wide hint names an inquiry: %+v", what, e)
		}
	}
	call := func(op int64, in InquiryCommand) *InquiryView {
		t.Helper()
		r, err := s.InquiryWorkspace(ctx, tenant, Operator{ID: op, Name: "op"}, in)
		if err != nil {
			t.Fatalf("%s: %v", in.Action, err)
		}
		return r.Item
	}

	saved := call(101, InquiryCommand{Action: "save", View: "SALES", Body: InquiryBody{CustomerID: "10", Products: []InquiryProduct{{Product: "Steel", Quantity: "1", Unit: "MT"}}}})
	expectHint("save")
	submitted := call(101, InquiryCommand{Action: "submit", View: "SALES", ID: saved.ID, Revision: saved.Revision})
	expectHint("submit")
	quote := InquiryQuoteBody{Company: "Factory", Currency: "USD", QuoteCategory: "FOB_USD", Incoterm: "FOB", Prices: []InquiryPrice{{ProductID: submitted.Body.Products[0].ID, Price: "1"}}}
	call(201, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: submitted.ID, Revision: submitted.Revision, Quote: quote, Submit: true})
	expectHint("quote")
	withdrawn := call(101, InquiryCommand{Action: "withdraw", View: "SALES", ID: submitted.ID, Revision: submitted.Revision})
	expectHint("withdraw")
	if _, err := s.InquiryWorkspace(ctx, tenant, Operator{ID: 101, Name: "op"}, InquiryCommand{Action: "delete", View: "SALES", ID: withdrawn.ID, Revision: withdrawn.Revision}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	expectHint("delete")
	if _, err := s.CreateSourcingCase(ctx, tenant, NewSourcingCase{CustomerID: 10, CustomerName: "Canonical", Lines: []SourcingLineInput{{Product: "Coil", Quantity: "2", QuantityUnit: "MT"}}}, Operator{ID: 101, Name: "op"}); err != nil {
		t.Fatal(err)
	}
	expectHint("an inquiry arriving from mail")

	if _, err := s.InquiryWorkspace(ctx, tenant, Operator{ID: 101}, InquiryCommand{Action: "save", View: "SALES", ID: saved.ID, Revision: saved.Revision + 99, Body: saved.Body}); err == nil {
		t.Fatal("stale revision accepted")
	}
	if _, err := s.InquiryWorkspace(ctx, tenant, Operator{ID: 101}, InquiryCommand{Action: "list", View: "SALES"}); err != nil {
		t.Fatal(err)
	}
	if e, ok := nextInquiryHint(500 * time.Millisecond); ok {
		t.Fatalf("a failed write or a read sent %+v", e)
	}
}

// Between the list query and the page read an inquiry can be withdrawn. The
// page read must not hand that inquiry to a buyer, as readInquiry would not.
func TestInquiryPageDropsRowsThatLeftTheView(t *testing.T) {
	customers := newListCustomers()
	s, _, tenant := listTestService(t, &customers)
	ids := seedInquiries(t, s, tenant, 0)
	withdrawn := inquiryID(ids[3])
	ctx := context.Background()
	buyer, err := s.readInquiryPage(ctx, tenant, Operator{ID: 201}, "PROCUREMENT", []int64{withdrawn})
	if err != nil || len(buyer) != 0 {
		t.Fatalf("a withdrawn inquiry reached the buyer's page: %v %+v", err, buyer)
	}
	sales, err := s.readInquiryPage(ctx, tenant, Operator{ID: 101}, "SALES", []int64{withdrawn})
	if err != nil || len(sales) != 1 || sales[0].State != "WITHDRAWN" {
		t.Fatalf("the salesperson lost their withdrawn inquiry: %v %+v", err, sales)
	}
}

// A legacy inquiry that saved only a customer name is linked to the customer
// the first time a list shows it, and shows the customer's current name.
func TestInquiryListLinksNameOnlyCustomers(t *testing.T) {
	customers := &listCustomersBatch{listCustomers: newListCustomers()}
	s, _, tenant := listTestService(t, customers)
	ids := seedInquiries(t, s, tenant, 0)
	named := ids[4]
	customers.names[10] = "Canonical Renamed"
	ctx := context.Background()
	r, err := s.listInquiryWorkspace(ctx, tenant, Operator{ID: 101}, InquiryCommand{Action: "list", View: "SALES", Page: 1, Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	var row *InquiryView
	for i := range r.Items {
		if r.Items[i].ID == named {
			row = &r.Items[i]
		}
	}
	if row == nil || row.Body.CustomerID != "10" || row.Body.Customer != "Canonical Renamed" {
		t.Fatalf("name-only inquiry not linked and renamed: %+v", row)
	}
	var stored int64
	if err := s.pool.QueryRow(ctx, `SELECT customer_id FROM sourcing_cases WHERE tenant_id=$1 AND id=$2`, tenant, inquiryID(named)).Scan(&stored); err != nil || stored != 10 {
		t.Fatalf("link not saved: %v %d", err, stored)
	}
}
