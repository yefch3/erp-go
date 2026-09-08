package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/shopspring/decimal"
)

type d2OfferScopes struct{}

func (d2OfferScopes) VisibleEmployees(context.Context, int64, string) (Visibility, error) {
	return Visibility{All: true}, nil
}
func (d2OfferScopes) HasPermission(_ context.Context, id int64, code string) (bool, error) {
	return id == 1 || id == 2 && code == "export:quotation:read", nil
}

type d2OfferSource struct{ inquiry OfferInquiry }

func (s *d2OfferSource) ReadInquiry(context.Context, int64) (OfferInquiry, error) {
	return s.inquiry, nil
}

type d2OfferRates struct{}

func (d2OfferRates) Latest(context.Context, string) (Rate, error) {
	return Rate{Rate: decimal.NewFromInt(1), Base: "USD", Source: "BASE", At: time.Now()}, nil
}
func (d2OfferRates) EffectiveRates(context.Context) ([]OfferRate, error) {
	return []OfferRate{{Base: "USD", Quote: "CNY", Value: "7.05", At: "2026-09-07T00:00:00Z"}}, nil
}

type d2OfferNumber struct{ n atomic.Int64 }

func (n *d2OfferNumber) Next(_ context.Context, kind string) (string, error) {
	return fmt.Sprintf("D2-%s-%d-%d", kind, time.Now().UnixNano(), n.n.Add(1)), nil
}

type d2Approvals struct{}

func (d2Approvals) Submit(context.Context, ApprovalSubmission) (int64, error)   { return 1, nil }
func (d2Approvals) MyDocuments(context.Context, int64, string) ([]int64, error) { return nil, nil }

type d2Files struct{}

func (d2Files) PresignPut(context.Context, string) (string, int32, error) {
	return "https://test.invalid/put", 60, nil
}
func (d2Files) PresignGet(context.Context, string) (string, error) {
	return "https://test.invalid/get", nil
}
func (d2Files) Stat(_ context.Context, key string) (int64, string, error) {
	if len(key) > 0 && key[len(key)-1:] == "x" {
		return 0, "", fmt.Errorf("not uploaded")
	}
	return 64, "application/pdf", nil
}
func TestD2OfferNegotiationConfirmationAndWithdrawal(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()%1000000000 + 2000000000
	actor := func(id int64) context.Context {
		return grpcx.WithOperator(ctx, grpcx.Operator{TenantID: tenant, EmployeeID: id, Name: "D2 Sales"})
	}
	source := &d2OfferSource{inquiry: OfferInquiry{ID: "901", Number: "D2-INQUIRY", OwnerID: "1", Owner: "D2 Sales", State: "INQUIRING"}}
	source.inquiry.Body.Delivery = "2026-10-01"
	source.inquiry.Body.Customer = "Negotiated customer"
	source.inquiry.Body.Products = []OfferProduct{{ID: "p1", Product: "Steel", Quantity: "100", Unit: "MT"}}
	svc := New(pool, Deps{Approvals: d2Approvals{}, Files: d2Files{}, OfferSource: source, Scopes: d2OfferScopes{}, Rates: d2OfferRates{}, Numbering: &d2OfferNumber{}})
	t.Cleanup(func() {
		for _, query := range []string{"DELETE FROM outbox_events WHERE tenant_id=$1", "DELETE FROM contract_attachments WHERE tenant_id=$1", "DELETE FROM customer_offers WHERE tenant_id=$1", "DELETE FROM contract_items WHERE tenant_id=$1", "DELETE FROM contract_versions WHERE tenant_id=$1", "DELETE FROM contracts WHERE tenant_id=$1", "DELETE FROM quotations WHERE tenant_id=$1", "DELETE FROM inquiry_withdrawal_fences WHERE tenant_id=$1"} {
			_, _ = pool.Exec(ctx, query, tenant)
		}
	})
	call := func(actorID int64, cmd OfferCommand) (OfferView, error) {
		t.Helper()
		data, _ := json.Marshal(cmd)
		out, e := svc.CustomerOffer(actor(actorID), string(data))
		var result OfferView
		if e == nil {
			e = json.Unmarshal([]byte(out), &result)
		}
		return result, e
	}
	first, err := call(1, OfferCommand{Action: "get", CaseID: "901"})
	if err != nil || first.Status != "PENDING" {
		t.Fatal(first, err)
	}
	body := first.Body
	body.Lines[0].Quantity = "60"
	body.Lines[0].UnitPrice = "125"
	if _, err := call(2, OfferCommand{Action: "save", CaseID: "901", Body: body}); err == nil {
		t.Fatal("manager modified owner's quote")
	}
	saved, err := call(1, OfferCommand{Action: "save", CaseID: "901", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Body.Total != "7500.00" || source.inquiry.Body.Products[0].Quantity != "100" {
		t.Fatal("negotiation changed original inquiry", saved)
	}
	if _, err := call(1, OfferCommand{Action: "save", CaseID: "901", Body: body}); err == nil {
		t.Fatal("stale save accepted")
	}
	confirmed, err := call(1, OfferCommand{Action: "confirm", CaseID: "901", Revision: saved.Revision})
	if err != nil {
		t.Fatal(err)
	}
	again, err := call(1, OfferCommand{Action: "confirm", CaseID: "901", Revision: saved.Revision})
	if err != nil || again.ContractID != confirmed.ContractID {
		t.Fatal("confirmation not idempotent", again, err)
	}
	contract, err := svc.GetContract(ctx, tenant, offerID(confirmed.ContractID), 0)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Version.TotalAmount != "7500.00" || len(contract.Items) != 1 || contract.Items[0].Qty != "60.0000" {
		t.Fatalf("contract differs from final quote: %+v", contract)
	}
	if _, err := call(1, OfferCommand{Action: "save", CaseID: "901", Revision: confirmed.Revision, Body: body}); err == nil {
		t.Fatal("confirmed quote overwritten")
	}
	if err := svc.SetInquiryAvailability(ctx, tenant, 901, false); err == nil {
		t.Fatal("confirmed inquiry withdrawn")
	}
	source.inquiry.Body.Products[0].Quantity = "999"
	frozen, err := call(1, OfferCommand{Action: "get", CaseID: "901"})
	if err != nil || frozen.Source.Body.Products[0].Quantity != "100" {
		t.Fatal("confirmed source snapshot overwritten", err)
	}
	contractID := offerID(confirmed.ContractID)
	owner := Operator{ID: 1, Name: "D2 Sales"}
	if _, err := svc.SignContract(actor(1), tenant, contractID, "", "", "", owner); err == nil {
		t.Fatal("started before approval")
	}
	if _, _, err := svc.SubmitContract(actor(1), tenant, contractID, owner); err != nil {
		t.Fatal(err)
	}
	claim := func(context.Context, pgx.Tx) error { return nil }
	if state, err := svc.ApplyApprovalDecision(ctx, tenant, contractID, "RETURNED", claim); err != nil || state != "DRAFT" {
		t.Fatal("return", state, err)
	}
	if _, _, err := svc.SubmitContract(actor(1), tenant, contractID, owner); err != nil {
		t.Fatal("resubmit", err)
	}
	if state, err := svc.ApplyApprovalDecision(ctx, tenant, contractID, "APPROVED", claim); err != nil || state != "PENDING_SIGN" {
		t.Fatal("approval", state, err)
	}
	if _, err := svc.SignContract(actor(1), tenant, contractID, "", "", "", owner); err == nil {
		t.Fatal("started without signed file")
	}
	key := fmt.Sprintf("contracts/%d/%d/signed.pdf", tenant, contractID)
	if _, err := svc.RegisterContractFile(actor(1), tenant, contractID, FileInput{Key: key + "x", FileName: "missing.pdf", Kind: "SIGNED"}, owner); err == nil {
		t.Fatal("registered nonexistent upload")
	}
	file, err := svc.RegisterContractFile(actor(1), tenant, contractID, FileInput{Key: key, FileName: "signed.pdf", Kind: "SIGNED"}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if state, err := svc.SignContract(actor(1), tenant, contractID, "", "", "", owner); err != nil || state != "EXECUTING" {
		t.Fatal("start", state, err)
	}
	if _, err := svc.RemoveContractFile(actor(1), tenant, file.Row.ID, owner); err == nil {
		t.Fatal("deleted executing contract evidence")
	}
	if state, err := svc.SignContract(actor(1), tenant, contractID, "", "", "", owner); err != nil || state != "EXECUTING" {
		t.Fatal("retry start", state, err)
	}

	if state, err := svc.CompleteContract(actor(2), tenant, contractID, Operator{ID: 2}); err == nil {
		t.Fatal("other employee completed", state)
	}
	if state, err := svc.CompleteContract(actor(1), tenant, contractID, owner); err != nil || state != "COMPLETED" {
		t.Fatal("complete", state, err)
	}
	if _, err := svc.UpdateContract(actor(1), tenant, contractID, Terms{}, nil, ContractEditMeta{}, owner); err == nil {
		t.Fatal("edited completed contract")
	}

	if _, _, _, err := svc.PresignContractFile(actor(1), tenant, contractID, "late.pdf", "application/pdf", owner); err == nil {
		t.Fatal("completed contract accepted upload")
	}
	if _, err := svc.RegisterContractFile(actor(1), tenant, contractID, FileInput{Key: fmt.Sprintf("contracts/%d/%d/late.pdf", tenant, contractID), FileName: "late.pdf"}, owner); err == nil {
		t.Fatal("completed contract registered attachment")
	}
	source.inquiry.ID = "902"
	draft, err := call(1, OfferCommand{Action: "save", CaseID: "902", Body: body})
	if err != nil || draft.Revision != 1 {
		t.Fatal(draft, err)
	}
	if err := svc.SetInquiryAvailability(ctx, tenant, 902, false); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM customer_offers WHERE tenant_id=$1 AND case_id=902", tenant).Scan(&count); err != nil || count != 0 {
		t.Fatal("withdrawal retained unconfirmed price", count, err)
	}
	if _, err := call(1, OfferCommand{Action: "save", CaseID: "902", Body: body}); err == nil {
		t.Fatal("withdrawal fence bypassed")
	}
	source.inquiry.ID = "903"
	recoverDraft, err := call(1, OfferCommand{Action: "save", CaseID: "903", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	recoverOffer, err := call(1, OfferCommand{Action: "confirm", CaseID: "903", Revision: recoverDraft.Revision})
	if err != nil {
		t.Fatal(err)
	}
	recoverID := offerID(recoverOffer.ContractID)
	if _, err := pool.Exec(ctx, `UPDATE contracts SET approval_request_key='durable-attempt' WHERE tenant_id=$1 AND id=$2`, tenant, recoverID); err != nil {
		t.Fatal(err)
	}
	if state, err := svc.ApplyApprovalDecision(ctx, tenant, recoverID, "APPROVED", claim, ApprovalProof{InstanceID: 22, RequestKey: "old-attempt"}); err != nil || state != "DRAFT" {
		t.Fatal("stale approval changed contract", state, err)
	}
	if state, err := svc.ApplyApprovalDecision(ctx, tenant, recoverID, "APPROVED", claim, ApprovalProof{InstanceID: 23, RequestKey: "durable-attempt"}); err != nil || state != "PENDING_SIGN" {
		t.Fatal("remote decision did not recover interrupted local submit", state, err)
	}

}
