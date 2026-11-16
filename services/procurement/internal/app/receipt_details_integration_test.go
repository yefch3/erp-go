package app

import (
	"context"
	"testing"
)

func TestReceiptDetailsAreAtomicScopedAndIdempotent(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	ctx := context.Background()
	meta := ReceiptDetails{ActualDeliveryDate: "2026-11-12", RequestKey: "delivery-batch-1", Proofs: []ReceiptProof{{FileName: "handoff.txt", ContentType: "text/plain", Data: []byte("independent delivery evidence")}}}
	lines := []ReceiptLine{{POItemID: f.item, Qty: "4"}}
	if _, err := f.svc.ReceiveOrder(ctx, f.tenant, f.po, 0, lines, "batch 1", Operator{ID: 77}, meta); err == nil {
		t.Fatal("uninspected delivery accepted")
	}
	f.pass(t)
	first, err := f.svc.ReceiveOrder(ctx, f.tenant, f.po, 0, lines, "batch 1", Operator{ID: 77}, meta)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.svc.ReceiveOrder(ctx, f.tenant, f.po, 0, lines, "batch 1", Operator{ID: 77}, meta)
	if err != nil || first != again {
		t.Fatalf("retry first=%s again=%s err=%v", first, again, err)
	}
	if _, err = f.svc.ReceiveOrder(ctx, f.tenant, f.po, 0, []ReceiptLine{{POItemID: f.item, Qty: "3"}}, "batch 1", Operator{ID: 77}, meta); err == nil {
		t.Fatal("changed retry accepted")
	}
	receipts, err := f.svc.OrderReceipts(ctx, f.tenant, f.po)
	if err != nil || len(receipts) != 1 {
		t.Fatalf("duplicate receipts: %+v %v", receipts, err)
	}
	detail, err := f.svc.ReceiptDetails(ctx, f.tenant, f.po)
	if err != nil {
		t.Fatal(err)
	}
	saved := detail[receipts[0].ID]
	if saved.ActualDeliveryDate != "2026-11-12" || len(saved.Proofs) != 1 {
		t.Fatalf("saved=%+v", saved)
	}
	proof, err := f.svc.DownloadReceiptProof(ctx, f.tenant, f.po, saved.Proofs[0].ID)
	if err != nil || string(proof.Data) != "independent delivery evidence" {
		t.Fatalf("proof=%+v err=%v", proof, err)
	}
	if _, err = f.svc.DownloadReceiptProof(ctx, f.tenant+1, f.po, proof.ID); err == nil {
		t.Fatal("cross-tenant evidence read")
	}
	if _, err = f.svc.DownloadReceiptProof(ctx, f.tenant, f.po+1, proof.ID); err == nil {
		t.Fatal("cross-order evidence read")
	}
	items, _ := f.svc.OrderItems(ctx, f.tenant, f.po)
	if items[0].ReceivedQty != "4.0000" {
		t.Fatalf("quantity=%s", items[0].ReceivedQty)
	}
	meta.RequestKey = "delivery-batch-2"
	if _, err = f.svc.ReceiveOrder(ctx, f.tenant, f.po, 0, []ReceiptLine{{POItemID: f.item, Qty: "6"}}, "batch 2", Operator{ID: 77}, meta); err != nil {
		t.Fatal(err)
	}
	head, _ := f.svc.GetOrder(ctx, f.tenant, f.po)
	if head.Status != "RECEIVED" {
		t.Fatalf("status=%s", head.Status)
	}
}
func TestReceiptDetailsRejectInvalidMetadata(t *testing.T) {
	for _, in := range []ReceiptDetails{{ActualDeliveryDate: "invalid"}, {Proofs: []ReceiptProof{{FileName: "empty.pdf"}}}, {Proofs: []ReceiptProof{{FileName: "large.pdf", Data: make([]byte, (2<<20)+1)}}}} {
		if _, _, err := prepareReceiptDetails(in, 0, nil, ""); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
}
