package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"os"
	"testing"
	"time"
)

type inquiryCustomerFixture struct{ allowed bool }

func (f *inquiryCustomerFixture) Check(_ context.Context, id int64) (string, error) {
	if f.allowed && id == 10 {
		return "Canonical", nil
	}
	return "", apierr.Permission("CUSTOMER_DENIED", "denied")
}
func (f *inquiryCustomerFixture) VisibleIDs(context.Context, int64) ([]int64, bool, error) {
	if f.allowed {
		return []int64{10}, false, nil
	}
	return nil, false, nil
}
func TestInquiryRejectsForgedCustomerBeforeWriting(t *testing.T) {
	s := &Service{customers: &inquiryCustomerFixture{allowed: true}}
	for _, cid := range []string{"20", "0", ""} {
		cmd := InquiryCommand{Body: InquiryBody{CustomerID: cid, Customer: "Forged"}}
		if _, err := s.saveInquiry(context.Background(), 1, Operator{ID: 101}, cmd); err == nil {
			t.Fatalf("save accepted customer %q", cid)
		}
		if _, err := s.updateInquiryBasic(context.Background(), 1, Operator{ID: 101}, cmd); err == nil {
			t.Fatalf("update accepted customer %q", cid)
		}
	}
}
func TestInquiryCustomerRemovalIntegration(t *testing.T) {
	dsn := os.Getenv("CUSTOMER_PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("CUSTOMER_PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano() / 1000
	customers := &inquiryCustomerFixture{allowed: true}
	s := New(pool, Deps{Customers: customers, Scopes: inquiryManagerAccess{}})
	op := Operator{ID: 101, Name: "Sales"}
	in := InquiryCommand{Action: "save", View: "SALES", Body: InquiryBody{CustomerID: "10", Customer: "Forged", Products: []InquiryProduct{{Product: "Steel", Specification: "Q235", Quantity: "1", Unit: "MT"}}}}
	result, err := s.InquiryWorkspace(ctx, tenant, op, in)
	if err != nil {
		t.Fatal(err)
	}
	id := result.Item.ID
	defer func() {
		if _, err := pool.Exec(ctx, "DELETE FROM sourcing_cases WHERE tenant_id=$1", tenant); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	if result.Item.Body.Customer != "Canonical" {
		t.Fatal("untrusted customer name persisted")
	}
	list, err := s.InquiryWorkspace(ctx, tenant, op, InquiryCommand{Action: "list", View: "SALES"})
	if err != nil || list.Total != 1 {
		t.Fatalf("initial list: %v %v", list, err)
	}
	customers.allowed = false
	list, err = s.InquiryWorkspace(ctx, tenant, op, InquiryCommand{Action: "list", View: "SALES"})
	if err != nil || list.Total != 0 || len(list.Items) != 0 {
		t.Fatalf("revoked list: %v %v", list, err)
	}
	if _, err = s.InquiryWorkspace(ctx, tenant, op, InquiryCommand{Action: "get", View: "SALES", ID: id}); err == nil {
		t.Fatal("removed owner read inquiry")
	}
	if _, err = s.InquiryWorkspace(ctx, tenant, op, InquiryCommand{Action: "save", View: "SALES", ID: id, Revision: result.Item.Revision, Body: result.Item.Body}); err == nil {
		t.Fatal("removed owner modified inquiry")
	}
	customers.allowed = true
	if _, err = s.InquiryWorkspace(ctx, tenant, op, InquiryCommand{Action: "get", View: "SALES", ID: id}); err != nil {
		t.Fatal("added owner did not regain access", err)
	}
	var customerID int64
	if err = pool.QueryRow(ctx, "SELECT customer_id FROM sourcing_cases WHERE tenant_id=$1 AND id=$2", tenant, inquiryID(id)).Scan(&customerID); err != nil || customerID != 10 {
		t.Fatal("historical customer changed")
	}
	other, err := s.InquiryWorkspace(ctx, tenant+1, op, InquiryCommand{Action: "list", View: "SALES"})
	if err != nil || other.Total != 0 {
		t.Fatal("cross-tenant inquiry leak")
	}
}
