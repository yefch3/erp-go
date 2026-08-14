package app

import (
	"context"
	"testing"
)

func TestSendProcurementRFQRequiresConfiguredSharedSender(t *testing.T) {
	service := &Service{}
	if _, err := service.SendProcurementRFQ(context.Background(), 1, 0, "Supplier", "supplier@example.com", "RFQ", "Please quote", "rfq.xlsx", []byte("x")); err == nil {
		t.Fatal("expected an unconfigured shared-sender error")
	}
}
