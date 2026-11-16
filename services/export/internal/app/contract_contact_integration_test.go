package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestManualContractCapturesCompleteBasicInformation(t *testing.T) {
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
	svc := New(pool, Deps{Customers: dueCustomerStub{}, Products: dueProductStub{}, Rates: d2OfferRates{}, Numbering: &dueNumberingStub{}})
	op := Operator{ID: 5, Name: "Creation test sales"}
	terms := Terms{CustomerContact: "测试联系人", BuyerName: "Actual buyer", BuyerAddress: "Buyer contract address", SellerName: "Actual seller", SellerAddress: "Seller contract address", Incoterm: "CIF", PaymentMethod: "30% advance, 70% after shipment", PortOfLoading: "Ningbo", PortOfDischarge: "New York", DeliveryDate: "2026-12-20", ReceivableDueDate: "2026-12-30", Text: "Complete terms entered at creation"}
	v, err := svc.CreateContract(ctx, tenant, DirectContractInput{CustomerID: 7, Currency: "USD", ExternalContractNo: "CREATE-ALL-FIELDS", Terms: terms, Items: []ItemInput{{ProductName: "Creation snapshot test", UomCode: "KG", Qty: "2", UnitPrice: "3"}}}, op)
	if err != nil {
		t.Fatal(err)
	}
	v, err = svc.GetContract(ctx, tenant, v.Contract.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if v.Version.CustomerContact == nil || *v.Version.CustomerContact != terms.CustomerContact {
		t.Fatal("contact was not stored in the contract snapshot")
	}
	if v.Version.BuyerName != terms.BuyerName || v.Version.BuyerAddress != terms.BuyerAddress || v.Version.SellerName != terms.SellerName || v.Version.SellerAddress != terms.SellerAddress || v.Version.Incoterm != terms.Incoterm || v.Version.PaymentMethod != terms.PaymentMethod || v.Version.PortOfLoading != terms.PortOfLoading || v.Version.PortOfDischarge != terms.PortOfDischarge || v.Version.DeliveryDate != terms.DeliveryDate || v.Version.Terms != terms.Text {
		t.Fatal("creation lost basic contract information")
	}
	if v.Contract.ExternalContractNo != "CREATE-ALL-FIELDS" || v.Contract.ReceivableDueDate != terms.ReceivableDueDate || v.Contract.SalesEmployeeID != op.ID || v.Contract.SalesEmployee != op.Name || v.Contract.QuoteNo != "" {
		t.Fatal("creation header or automatic owner/source was incorrect")
	}
	terms.CustomerContact = "Updated contact"
	v, err = svc.UpdateContract(ctx, tenant, v.Contract.ID, terms, nil, ContractEditMeta{ExternalContractNo: "CREATE-ALL-FIELDS"}, op)
	if err != nil {
		t.Fatal(err)
	}
	if contactValue(v.Version.CustomerContact) != terms.CustomerContact {
		t.Fatal("draft contact edit was not preserved")
	}
	terms.CustomerContact = ""
	v, err = svc.UpdateContract(ctx, tenant, v.Contract.ID, terms, nil, ContractEditMeta{ExternalContractNo: "CREATE-ALL-FIELDS"}, op)
	if err != nil {
		t.Fatal(err)
	}
	if v.Version.CustomerContact == nil || *v.Version.CustomerContact != "" {
		t.Fatal("an explicitly cleared contact must remain distinguishable from a legacy snapshot")
	}
}
