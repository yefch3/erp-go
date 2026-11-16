package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

type partiesApprovalProbe struct{ calls int }

func (p *partiesApprovalProbe) Submit(context.Context, ApprovalSubmission) (int64, error) {
	p.calls++
	return 123, nil
}

func completeDraftParties(t *testing.T, svc *Service, ctx context.Context, tenant, contract int64) {
	t.Helper()
	_, err := svc.pool.Exec(ctx, `UPDATE contract_versions
		SET buyer_name='Test buyer',buyer_address='Test buyer address',seller_name='Test seller',seller_address='Test seller address'
		WHERE tenant_id=$1 AND contract_id=$2 AND status='DRAFT'`, tenant, contract)
	if err != nil {
		t.Fatal(err)
	}
}

func TestContractPartiesDraftAndSubmission(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tenant := time.Now().UnixNano()
	t.Cleanup(func() {
		for _, table := range []string{"contract_workflows", "contract_items", "contract_versions", "contracts"} {
			if _, err := pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenant); err != nil {
				t.Errorf("cleanup %s: %v", table, err)
			}
		}
	})
	approval := &partiesApprovalProbe{}
	svc := New(pool, Deps{Customers: dueCustomerStub{}, Products: dueProductStub{}, Rates: d2OfferRates{}, Numbering: &dueNumberingStub{}, Approvals: approval, Seller: Seller{Name: "Configured seller", Address: "Configured address"}})
	op := Operator{ID: 5, Name: "Sales"}
	in := DirectContractInput{CustomerID: 7, Currency: "USD", Terms: Terms{DeliveryDate: "2026-12-01"}, Items: []ItemInput{{ProductName: "Parties regression", UomCode: "KG", Qty: "1", UnitPrice: "2"}}}
	v, err := svc.CreateContract(ctx, tenant, in, op)
	if err != nil {
		t.Fatal(err)
	}
	if v.Version.BuyerName != "" || v.Version.BuyerAddress != "" || v.Version.SellerName != "" || v.Version.SellerAddress != "" {
		t.Fatal("draft fields were silently replaced with defaults")
	}
	if _, _, err := svc.SubmitContract(ctx, tenant, v.Contract.ID, op); apierr.CodeFromError(err) != "EX_CONTRACT_PARTIES_REQUIRED" {
		t.Fatalf("incomplete draft submitted: %v", err)
	}
	var key string
	if err := pool.QueryRow(ctx, `SELECT approval_request_key FROM contracts WHERE tenant_id=$1 AND id=$2`, tenant, v.Contract.ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if approval.calls != 0 || key != "" {
		t.Fatal("incomplete draft created an approval attempt")
	}
	terms := Terms{BuyerName: "Actual buyer", BuyerAddress: "Actual buyer address", SellerName: "Actual seller", SellerAddress: "Actual seller address", DeliveryDate: "2026-12-01"}
	v, err = svc.UpdateContract(ctx, tenant, v.Contract.ID, terms, nil, ContractEditMeta{}, op)
	if err != nil {
		t.Fatal(err)
	}
	if v.Version.BuyerName != terms.BuyerName || v.Version.BuyerAddress != terms.BuyerAddress || v.Version.SellerName != terms.SellerName || v.Version.SellerAddress != terms.SellerAddress {
		t.Fatal("party fields were not preserved")
	}
	// Clearing a field while drafting must stay blank, even when a configured
	// seller or an earlier snapshot exists.
	cleared := terms
	cleared.SellerName = ""
	cleared.BuyerName = ""
	v, err = svc.UpdateContract(ctx, tenant, v.Contract.ID, cleared, nil, ContractEditMeta{}, op)
	if err != nil {
		t.Fatal(err)
	}
	if v.Version.SellerName != "" || v.Version.BuyerName != "" {
		t.Fatal("cleared draft names were silently restored")
	}
	if _, _, err = svc.SubmitContract(ctx, tenant, v.Contract.ID, op); apierr.CodeFromError(err) != "EX_CONTRACT_PARTIES_REQUIRED" {
		t.Fatalf("cleared draft submitted: %v", err)
	}
	if _, err = svc.UpdateContract(ctx, tenant, v.Contract.ID, terms, nil, ContractEditMeta{}, op); err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.SubmitContract(ctx, tenant, v.Contract.ID, op); err != nil {
		t.Fatal(err)
	}
	if approval.calls != 1 {
		t.Fatalf("approval calls=%d", approval.calls)
	}
}
