package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sgao19/erp-go/pkg/apierr"
)

type quoteDeleteAccess struct{ d1Access }

func (a quoteDeleteAccess) HasPermission(ctx context.Context, id int64, permission string) (bool, error) {
	if id == 203 {
		return permission == "procurement:sourcing:read", nil
	}
	return a.d1Access.HasPermission(ctx, id, permission)
}

func TestInquiryQuoteDeletion(t *testing.T) {
	s, _, tenant := listTestService(t, nil)
	s.scopes = quoteDeleteAccess{}
	ctx := context.Background()
	call := func(actor int64, in InquiryCommand) *InquiryView {
		t.Helper()
		r, err := s.InquiryWorkspace(ctx, tenant, Operator{ID: actor, Name: "Quote tester"}, in)
		if err != nil {
			t.Fatalf("%s/%s actor %d: %v", in.View, in.Action, actor, err)
		}
		return r.Item
	}
	create := func() *InquiryView {
		t.Helper()
		v := call(101, InquiryCommand{Action: "save", View: "SALES", Body: InquiryBody{Customer: "Quote deletion", Products: []InquiryProduct{{Product: "Steel", Quantity: "5", Unit: "MT"}}}})
		return call(101, InquiryCommand{Action: "submit", View: "SALES", ID: v.ID, Revision: v.Revision})
	}
	inquiry := create()
	other := create()
	quoteBody := InquiryQuoteBody{Company: "Wrong factory", Currency: "USD", QuoteCategory: "FOB_USD", Prices: []InquiryPrice{{ProductID: inquiry.Body.Products[0].ID, Price: "500"}}}
	v := call(201, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: inquiry.ID, Revision: inquiry.Revision, Quote: quoteBody})
	quote := v.Quotes[0]
	deletion := InquiryCommand{Action: "deleteQuote", View: "PROCUREMENT", ID: inquiry.ID, Revision: inquiry.Revision, QuoteID: quote.ID, QuoteVersion: quote.Version}
	if !quote.CanDelete {
		t.Fatal("author cannot delete private quote")
	}
	for _, tc := range []struct {
		name    string
		actor   int64
		tenant  int64
		command InquiryCommand
	}{
		{"other draft author", 202, tenant, deletion},
		{"read only", 203, tenant, deletion},
		{"sales", 101, tenant, func() InquiryCommand { c := deletion; c.View = "SALES"; return c }()},
		{"logistics", 301, tenant, func() InquiryCommand { c := deletion; c.View = "LOGISTICS"; return c }()},
		{"other tenant", 201, tenant + 1, deletion},
		{"other inquiry", 201, tenant, func() InquiryCommand { c := deletion; c.ID = other.ID; return c }()},
		{"stale inquiry", 201, tenant, func() InquiryCommand { c := deletion; c.Revision--; return c }()},
		{"missing quote", 201, tenant, func() InquiryCommand { c := deletion; c.QuoteID = ""; return c }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.InquiryWorkspace(ctx, tc.tenant, Operator{ID: tc.actor}, tc.command); err == nil {
				t.Fatal("unauthorized/stale deletion succeeded")
			}
		})
	}
	// A saved edit invalidates a deletion made from an older browser view.
	v = call(201, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: inquiry.ID, Revision: inquiry.Revision, QuoteID: quote.ID, QuoteVersion: quote.Version, Quote: quoteBody, Submit: true})
	if _, err := s.InquiryWorkspace(ctx, tenant, Operator{ID: 201}, deletion); apierr.CodeFromError(err) != "INQUIRY_QUOTE_CHANGED" {
		t.Fatalf("stale quote delete: %v", err)
	}
	deletion.QuoteVersion = v.Quotes[0].Version
	if got := call(101, InquiryCommand{Action: "get", View: "SALES", ID: inquiry.ID}); len(got.Quotes) != 1 || got.Quotes[0].CanDelete {
		t.Fatal("sales visibility/deletion rights incorrect")
	}
	// Existing policy allows other procurement writers to maintain submitted quotes.
	v = call(202, InquiryCommand{Action: "get", View: "PROCUREMENT", ID: inquiry.ID})
	if !v.Quotes[0].CanDelete {
		t.Fatal("department writer cannot delete submitted quote")
	}
	v = call(202, deletion)
	if len(v.Quotes) != 0 || v.ProcurementCount != 0 {
		t.Fatal("quote or count remains after deletion")
	}
	for _, view := range []string{"SALES", "QUOTATIONS", "PROCUREMENT"} {
		actor := int64(101)
		if view == "PROCUREMENT" {
			actor = 201
		}
		got := call(actor, InquiryCommand{Action: "get", View: view, ID: inquiry.ID})
		if len(got.Quotes) != 0 || got.ProcurementCount != 0 {
			t.Fatalf("deleted quote visible to %s", view)
		}
	}
	list, err := s.InquiryWorkspace(ctx, tenant, Operator{ID: 201}, InquiryCommand{Action: "list", View: "PROCUREMENT"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range list.Items {
		if row.ID == inquiry.ID && row.ProcurementCount != 0 {
			t.Fatal("list count retains deleted quote")
		}
	}
	if _, err = s.InquiryWorkspace(ctx, tenant, Operator{ID: 201}, deletion); apierr.CodeFromError(err) != "INQUIRY_QUOTE_CHANGED" {
		t.Fatal("repeated delete", err)
	}
	if _, err = s.InquiryWorkspace(ctx, tenant, Operator{ID: 201}, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: inquiry.ID, Revision: inquiry.Revision, QuoteID: quote.ID, QuoteVersion: deletion.QuoteVersion, Quote: quoteBody}); apierr.CodeFromError(err) != "INQUIRY_QUOTE_CHANGED" {
		t.Fatal("deleted quote was editable", err)
	}
	var retained bool
	if err = s.pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM inquiry_quotes WHERE tenant_id=$1 AND id=$2`, tenant, inquiryID(quote.ID)).Scan(&retained); err != nil || !retained {
		t.Fatal("deletion did not retain quote history", err)
	}
	var before []byte
	var actor int64
	if err = s.pool.QueryRow(ctx, `SELECT before_json,operator_id FROM sourcing_case_changes WHERE tenant_id=$1 AND case_id=$2 AND section='INQUIRY_QUOTE' AND action='DELETED'`, tenant, inquiryID(inquiry.ID)).Scan(&before, &actor); err != nil {
		t.Fatal(err)
	}
	var audit struct {
		Body InquiryQuoteBody `json:"body"`
	}
	if err = json.Unmarshal(before, &audit); err != nil || audit.Body.Company != "Wrong factory" || actor != 202 {
		t.Fatal("deletion audit lost original quote or operator", err)
	}
	// A withdrawn inquiry cannot be targeted using a previously loaded quote.
	v = call(201, InquiryCommand{Action: "quote", View: "PROCUREMENT", ID: inquiry.ID, Revision: inquiry.Revision, Quote: quoteBody})
	deletion.QuoteID = v.Quotes[0].ID
	deletion.QuoteVersion = v.Quotes[0].Version
	call(101, InquiryCommand{Action: "withdraw", View: "SALES", ID: inquiry.ID, Revision: inquiry.Revision})
	if _, err = s.InquiryWorkspace(ctx, tenant, Operator{ID: 201}, deletion); apierr.CodeFromError(err) != "INQUIRY_CHANGED" {
		t.Fatal("withdrawn inquiry delete", err)
	}
}
