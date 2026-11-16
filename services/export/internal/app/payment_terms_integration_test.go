package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// Exercise the actual save/read paths: the form accepted free-form payment
// terms, but contract_versions.payment_method used to reject over 50 characters.
func TestLongPaymentTermsSurviveContractSaves(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	t.Cleanup(func() {
		for _, table := range []string{"contract_workflows", "contract_items", "contract_versions", "contracts", "quotation_items", "quotations"} {
			if _, err := pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenantID); err != nil {
				t.Errorf("clean up %s: %v", table, err)
			}
		}
	})
	svc := New(pool, Deps{
		Customers: dueCustomerStub{}, Products: dueProductStub{},
		Rates: d2OfferRates{}, Numbering: &dueNumberingStub{},
	})
	op := Operator{ID: 5, Name: "Sales"}
	items := []ItemInput{{ProductName: "Custom plate", UomCode: "KG", Qty: "10", UnitPrice: "2"}}
	for _, tc := range []struct {
		name, payment string
	}{
		{"empty", ""},
		{"old limit", strings.Repeat("T", 50)},
		{"over old limit", strings.Repeat("T", 51)},
		{"long English", strings.Repeat("30% deposit; balance payable within 60 days after shipment. ", 20)},
		{"long Chinese", strings.Repeat("预付百分之三十，余款在收到提单副本后六十日内付清。", 20)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, err := svc.CreateContract(ctx, tenantID, DirectContractInput{
				CustomerID: 7, Currency: "USD", Terms: Terms{PaymentMethod: tc.payment}, Items: items,
			}, op)
			if err != nil {
				t.Fatalf("create contract: %v", err)
			}
			if view.Version.PaymentMethod != tc.payment {
				t.Fatal("created contract did not preserve payment terms")
			}
			updatedPayment := tc.payment + " Payment by bank transfer."
			if _, err := svc.UpdateContract(ctx, tenantID, view.Contract.ID,
				Terms{PaymentMethod: updatedPayment}, items, ContractEditMeta{}, op); err != nil {
				t.Fatalf("update contract: %v", err)
			}
			read, err := svc.GetContract(ctx, tenantID, view.Contract.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			if read.Version.PaymentMethod != updatedPayment {
				t.Fatal("saved contract did not preserve updated payment terms")
			}
		})
	}

	// A quotation must also keep the full text before it is inherited by a
	// contract. This catches a fix that only widens one side of the conversion.
	payment := strings.Repeat("预付百分之三十，余款凭提单副本支付。", 30)
	var quotationID int64
	err = pool.QueryRow(ctx, `INSERT INTO quotations
		(tenant_id,quote_no,customer_id,customer_name,currency,payment_method,
		 fx_rate,fx_rate_at,fx_source,total_amount,base_amount,status,sales_employee_id)
		VALUES ($1,'QT-PAYMENT-TERMS',7,'ACME','USD',$2,1,now(),'TEST',20,20,'ACCEPTED',$3)
		RETURNING id`, tenantID, payment, op.ID).Scan(&quotationID)
	if err != nil {
		t.Fatalf("save quotation payment terms: %v", err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO quotation_items
		(tenant_id,quotation_id,line_no,product_id,product_name,qty,uom_code,unit_price,amount)
		VALUES ($1,$2,1,0,'Custom plate',10,'KG',2,20)`, tenantID, quotationID)
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.CreateContractFromQuotation(ctx, tenantID, quotationID, Terms{}, op)
	if err != nil {
		t.Fatalf("convert quotation with long payment terms: %v", err)
	}
	if view.Version.PaymentMethod != payment {
		t.Fatal("quotation conversion did not preserve payment terms")
	}
}
