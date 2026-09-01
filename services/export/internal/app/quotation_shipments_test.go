package app

import "testing"

func TestResolveQuotationShipmentsAddsFreightWithoutProductLines(t *testing.T) {
	in := QuotationInput{
		Currency: "USD",
		Shipments: []QuotationShipmentInput{
			{Currency: "USD", FreightAmount: "240.50", CarrierForwarder: "Forwarder A"},
			{Currency: "USD", FreightAmount: "0", CustomerManaged: true},
		},
	}

	total, err := resolveQuotationShipments(in)
	if err != nil {
		t.Fatalf("resolve quotation shipments: %v", err)
	}
	if got := total.StringFixed(2); got != "240.50" {
		t.Fatalf("freight total = %s, want 240.50", got)
	}
}

func TestResolveQuotationShipmentsRejectsInvalidFormalFreight(t *testing.T) {
	tests := []struct {
		name     string
		shipment QuotationShipmentInput
	}{
		{name: "currency mismatch", shipment: QuotationShipmentInput{Currency: "EUR", FreightAmount: "10", CarrierForwarder: "Forwarder A"}},
		{name: "missing carrier", shipment: QuotationShipmentInput{Currency: "USD", FreightAmount: "10"}},
		{name: "zero freight", shipment: QuotationShipmentInput{Currency: "USD", FreightAmount: "0", CarrierForwarder: "Forwarder A"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveQuotationShipments(QuotationInput{Currency: "USD", Shipments: []QuotationShipmentInput{tt.shipment}})
			if err == nil {
				t.Fatal("expected invalid shipment to be rejected")
			}
		})
	}
}
