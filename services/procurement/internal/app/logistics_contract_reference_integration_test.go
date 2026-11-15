package app

import "testing"

func TestLogisticsPayableSeparatesOptionalContractAndTransportBatch(t *testing.T) {
	ctx, svc, tenant, cleanup := reconTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	first := ExternalPayableInput{BusinessType: "LOGISTICS", SourceBusinessID: 7701, PayeeID: 31, PayeeName: "Forwarder", ExportContractNo: "CT-LOG", TransportBatchNo: 1, Currency: "USD", Amount: "105", SignedContractKey: "contracts/one.pdf", SignedContractName: "one.pdf"}
	a, err := svc.CreateExternalPayable(ctx, tenant, first, op)
	if err != nil || a.BusinessDocumentNo != "" || a.TransportBatchNo != 1 || a.ExportContractNo != "CT-LOG" {
		t.Fatalf("blank optional contract: %+v, %v", a, err)
	}
	second := first
	second.SourceBusinessID, second.TransportBatchNo, second.BusinessDocumentNo = 7702, 2, "FWD-SIGNED-2"
	b, err := svc.CreateExternalPayable(ctx, tenant, second, op)
	if err != nil || b.POID == a.POID || b.PONo == a.PONo || b.BusinessDocumentNo != "FWD-SIGNED-2" || b.TransportBatchNo != 2 {
		t.Fatalf("distinct signed contract and batch: %+v, %v", b, err)
	}
	first.BusinessDocumentNo = "FWD-SIGNED-1"
	again, err := svc.CreateExternalPayable(ctx, tenant, first, op)
	if err != nil || again.POID != a.POID || again.BusinessDocumentNo != "FWD-SIGNED-1" {
		t.Fatalf("contract replacement created another payable: %+v, %v", again, err)
	}
	if _, err = svc.RecordPOPayment(ctx, tenant, POPaymentInput{POID: a.POID, Amount: "105", PaidAt: "2026-11-14"}, op); err == nil {
		t.Fatal("unverified payment must remain blocked")
	}
	if _, err = svc.VerifyOrderContract(ctx, tenant, a.POID, op); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.RecordPOPayment(ctx, tenant, POPaymentInput{POID: a.POID, Amount: "105", PaidAt: "2026-11-14"}, op); err != nil {
		t.Fatal(err)
	}
	untouched, err := svc.GetExternalPayable(ctx, tenant, "LOGISTICS", second.SourceBusinessID)
	if err != nil || untouched.PaidAmount != "0" {
		t.Fatalf("first batch payment affected second batch: %+v, %v", untouched, err)
	}
}
