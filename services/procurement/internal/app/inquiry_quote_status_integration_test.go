package app

import (
	"context"
	"testing"

	"github.com/sgao19/erp-go/pkg/apierr"
)

func TestInquiryPersonalQuoteStatus(t *testing.T) {
	for _, kind := range []string{"PROCUREMENT", "LOGISTICS"} {
		t.Run(kind, func(t *testing.T) {
			s, _, tenant := listTestService(t, nil)
			s.scopes = d1Access{}
			ctx := context.Background()
			a, b := int64(201), int64(202)
			if kind == "LOGISTICS" {
				a, b = 301, 302
			}
			call := func(actor int64, in InquiryCommand) *InquiryView {
				t.Helper()
				r, err := s.InquiryWorkspace(ctx, tenant, Operator{ID: actor, Name: "Status tester"}, in)
				if err != nil {
					t.Fatalf("actor %d %s/%s: %v", actor, in.View, in.Action, err)
				}
				return r.Item
			}
			v := call(101, InquiryCommand{Action: "save", View: "SALES", Body: InquiryBody{Customer: "Issue 606", Products: []InquiryProduct{{Product: "Steel", Quantity: "5", Unit: "MT"}}}})
			v = call(101, InquiryCommand{Action: "submit", View: "SALES", ID: v.ID, Revision: v.Revision})
			status := func(actor int64, want string) {
				t.Helper()
				detail := call(actor, InquiryCommand{Action: "get", View: kind, ID: v.ID})
				if detail.PersonalQuoteStatus != want {
					t.Fatalf("actor %d detail status %q, want %q", actor, detail.PersonalQuoteStatus, want)
				}
				for _, filter := range []string{"", "WAITING", "MY_QUOTED", "UPDATED", "QUOTED"} {
					r, err := s.InquiryWorkspace(ctx, tenant, Operator{ID: actor}, InquiryCommand{Action: "list", View: kind, Keyword: v.Number, State: filter, Page: 1, Size: 100})
					if err != nil {
						t.Fatal(err)
					}
					matches := filter == "" || filter == want
					if filter == "QUOTED" {
						matches = (kind == "PROCUREMENT" && detail.ProcurementCount > 0) || (kind == "LOGISTICS" && detail.LogisticsCount > 0)
					}
					if matches && (r.Total != 1 || len(r.Items) != 1 || r.Items[0].PersonalQuoteStatus != want) {
						t.Fatalf("actor %d filter %q: %+v, want %s", actor, filter, r, want)
					}
					if !matches && (r.Total != 0 || len(r.Items) != 0) {
						t.Fatalf("actor %d matched someone else's filter %s: %+v", actor, filter, r)
					}
				}
			}
			body := InquiryQuoteBody{Company: "Factory A", Currency: "USD", QuoteCategory: "FOB_USD", Prices: []InquiryPrice{{ProductID: v.Body.Products[0].ID, Price: "500"}}}
			if kind == "LOGISTICS" {
				body = InquiryQuoteBody{Company: "Forwarder A", Incoterm: "CFR", Charges: []InquiryCharge{{Name: "Ocean", Amount: "10", Quantity: "1", Currency: "USD"}}}
			}
			quote := func(actor int64, id string, version int64, submit bool, body InquiryQuoteBody) *InquiryView {
				t.Helper()
				return call(actor, InquiryCommand{Action: "quote", View: kind, ID: v.ID, Revision: v.Revision, QuoteID: id, QuoteVersion: version, Submit: submit, Quote: body})
			}
			find := func(actor int64, id string) InquiryQuoteView {
				t.Helper()
				for _, q := range call(actor, InquiryCommand{Action: "get", View: kind, ID: v.ID}).Quotes {
					if q.ID == id {
						return q
					}
				}
				t.Fatalf("missing quote %s", id)
				return InquiryQuoteView{}
			}
			status(a, "WAITING")
			status(b, "WAITING")
			draft := quote(a, "", 0, false, body).Quotes[0]
			status(a, "WAITING") // a saved draft is not a successful submission
			otherBody := body
			otherBody.Company = "Company B"
			bv := quote(b, "", 0, true, otherBody)
			qb := bv.Quotes[len(bv.Quotes)-1]
			status(a, "WAITING")
			status(b, "MY_QUOTED") // department quote ≠ my quote
			quote(a, draft.ID, draft.Version, true, body)
			status(a, "MY_QUOTED")
			status(b, "UPDATED")
			otherBody.Remark = "B changed their own quote"
			quote(b, qb.ID, qb.Version, false, otherBody)
			status(a, "UPDATED")
			status(b, "MY_QUOTED")
			status(a, "UPDATED") // reading does not silently clear a coworker's update
			qa := find(a, draft.ID)
			if qa.Body.Company != "Factory A" && qa.Body.Company != "Forwarder A" {
				t.Fatal("B overwrote A's record")
			}
			if kind == "PROCUREMENT" {
				for _, peer := range call(b, InquiryCommand{Action: "get", View: kind, ID: v.ID}).Quotes {
					if peer.ID == qa.ID {
						t.Fatal("coworker's factory quote is visible")
					}
				}
			} else if find(b, qa.ID).CanEdit {
				t.Fatal("coworker's submitted shipping quote is editable")
			}
			for _, submit := range []bool{false, true} {
				_, err := s.InquiryWorkspace(ctx, tenant, Operator{ID: b}, InquiryCommand{Action: "quote", View: kind, ID: v.ID, Revision: v.Revision, QuoteID: qa.ID, QuoteVersion: qa.Version, Quote: otherBody, Submit: submit})
				if apierr.CodeFromError(err) != "INQUIRY_QUOTE_AUTHOR" {
					t.Fatalf("coworker overwrite: %v", err)
				}
			}
			_, staleErr := s.InquiryWorkspace(ctx, tenant, Operator{ID: a}, InquiryCommand{Action: "quote", View: kind, ID: v.ID, Revision: v.Revision, QuoteID: qa.ID, QuoteVersion: qa.Version - 1, Quote: body})
			if apierr.CodeFromError(staleErr) != "INQUIRY_QUOTE_CHANGED" {
				t.Fatalf("stale own quote accepted: %v", staleErr)
			}
			if find(a, qa.ID).Version != qa.Version {
				t.Fatal("blocked edit changed the original version")
			}
			body.Remark = "A saved their own submitted quote"
			quote(a, qa.ID, qa.Version, false, body)
			status(a, "MY_QUOTED")
			status(b, "UPDATED")
			quote(b, "", 0, false, otherBody)
			status(a, "MY_QUOTED") // a coworker's new draft does not trigger UPDATED
			// Before #606, submitted quotes could be edited by a coworker. A
			// legacy edit on my record must still appear as the other's update.
			_, legacyErr := s.pool.Exec(ctx, `UPDATE inquiry_quotes SET updated_by=$4,updated_at=clock_timestamp(),version=version+1 WHERE tenant_id=$1 AND case_id=$2 AND id=$3`, tenant, inquiryID(v.ID), inquiryID(qa.ID), b)
			if legacyErr != nil {
				t.Fatal(legacyErr)
			}
			status(a, "UPDATED")
			own := find(a, qa.ID)
			quote(a, own.ID, own.Version, false, body)
			status(a, "MY_QUOTED")
			// Neither historical revisions, soft-deleted quotes nor another tenant
			// may alter this employee's current progress, even with later times.
			for _, extra := range []struct {
				tenant, revision int64
				deleted          bool
			}{{tenant, v.Revision - 1, false}, {tenant, v.Revision, true}, {tenant + 1, v.Revision, false}} {
				_, err := s.pool.Exec(ctx, `INSERT INTO inquiry_quotes(tenant_id,case_id,inquiry_revision,kind,body,created_by,created_by_name,updated_by,updated_by_name,submitted_at,updated_at,deleted_at) VALUES($1,$2,$3,$4,'{}',$5,'other',$5,'other','2099-01-01','2099-01-01',CASE WHEN $6 THEN now() ELSE NULL END)`, extra.tenant, inquiryID(v.ID), extra.revision, kind, b, extra.deleted)
				if err != nil {
					t.Fatal(err)
				}
			}
			status(a, "MY_QUOTED")
			// Editing a quote of the other department does not count as an update.
			otherKind, otherActor := "LOGISTICS", int64(301)
			otherQuote := InquiryQuoteBody{Company: "Forwarder", Incoterm: "CFR", Charges: []InquiryCharge{{Name: "Ocean", Amount: "12", Quantity: "1", Currency: "USD"}}}
			if kind == "LOGISTICS" {
				otherKind, otherActor = "PROCUREMENT", 201
				otherQuote = InquiryQuoteBody{Company: "Factory", Currency: "USD", QuoteCategory: "FOB_USD", Prices: []InquiryPrice{{ProductID: v.Body.Products[0].ID, Price: "600"}}}
			}
			call(otherActor, InquiryCommand{Action: "quote", View: otherKind, ID: v.ID, Revision: v.Revision, Quote: otherQuote, Submit: true})
			status(a, "MY_QUOTED")
			// Removing all my current submitted quotes means I must quote again.
			_, err := s.pool.Exec(ctx, `UPDATE inquiry_quotes SET deleted_at=now() WHERE tenant_id=$1 AND case_id=$2 AND kind=$3 AND created_by=$4`, tenant, inquiryID(v.ID), kind, a)
			if err != nil {
				t.Fatal(err)
			}
			status(a, "WAITING")
			oldRevision := v.Revision
			v = call(101, InquiryCommand{Action: "withdraw", View: "SALES", ID: v.ID, Revision: v.Revision})
			v = call(101, InquiryCommand{Action: "submit", View: "SALES", ID: v.ID, Revision: v.Revision})
			status(a, "WAITING")
			status(b, "WAITING")
			_, err = s.InquiryWorkspace(ctx, tenant, Operator{ID: a}, InquiryCommand{Action: "quote", View: kind, ID: v.ID, Revision: oldRevision, Quote: body, Submit: true})
			if err == nil {
				t.Fatal("old revision accepted")
			}
		})
	}
}
