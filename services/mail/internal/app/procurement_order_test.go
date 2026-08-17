package app

import (
	"context"
	"testing"
)

func TestSendProcurementOrderRejectsInvalidSupplierDocument(t *testing.T) {
	files := &countingFiles{}
	svc := &Service{files: files}
	_, err := svc.SendProcurementOrder(context.Background(), 1, 7, "Mill", "mill@example.com", "PO", "Attached", []ProcurementOrderAttachment{{FileName: "PO.pdf", ContentType: "application/pdf", Data: []byte("not a pdf")}})
	if err == nil {
		t.Fatal("expected invalid PDF to be rejected")
	}
	if files.puts != 0 {
		t.Fatalf("invalid document was uploaded %d times", files.puts)
	}
}
