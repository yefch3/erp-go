package httpapi

import (
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"testing"
)

func progressFixture() *exv1.GetContractResponse {
	return &exv1.GetContractResponse{Contract: &exv1.Contract{EntrySource: "STANDARD"}, Items: []*exv1.ContractItem{{Id: 11, LineNo: 1, ProductName: "Steel", Spec: "3mm", UomCode: "MT", Qty: "10"}, {Id: 12, LineNo: 2, ProductName: "Steel", Spec: "5mm", UomCode: "MT", Qty: "20"}}, Shipments: []*exv1.ShipmentProgress{{LineNo: 1, Qty: "10", ShippedQty: "0", RemainingQty: "10"}, {LineNo: 2, Qty: "20", ShippedQty: "0", RemainingQty: "20"}}}
}
func TestContractDispatchProgressReconcilesTransportWithoutDoubleCounting(t *testing.T) {
	view := progressFixture()
	lines := []*shippingv1.ContractDispatchLine{{ContractItemId: 11, ProductName: "Steel", Specification: "3mm", UomCode: "MT", Quantity: "4"}}
	if !mergeContractDispatchProgress(view, lines) || view.Shipments[0].ShippedQty != "4.0000" || view.Shipments[0].RemainingQty != "6.0000" || view.Shipments[1].ShippedQty != "0.0000" {
		t.Fatalf("wrong partial dispatch: %+v", view.Shipments)
	}
	view.Shipments[0].ShippedQty = "4"
	if !mergeContractDispatchProgress(view, lines) || view.Shipments[0].ShippedQty != "4.0000" {
		t.Fatal("warehouse and voyage were counted twice")
	}
	lines[0].Quantity = "10"
	if !mergeContractDispatchProgress(view, lines) || view.Shipments[0].RemainingQty != "0.0000" {
		t.Fatal("completed transport did not cover contract")
	}
	lines[0].ContractItemId = 999
	view.Shipments[0].ShippedQty = "0"
	if !mergeContractDispatchProgress(view, lines) || view.Shipments[0].ShippedQty != "10.0000" {
		t.Fatal("same goods lost after contract version change")
	}
}
func TestContractDispatchProgressRefusesAmbiguousAndInvalidFacts(t *testing.T) {
	view := progressFixture()
	view.Items[1].Spec = "3mm"
	view.Items[1].ProductId = 2
	if mergeContractDispatchProgress(view, []*shippingv1.ContractDispatchLine{{ContractItemId: -1, ProductName: "Steel", Specification: "3mm", UomCode: "MT", Quantity: "10"}}) {
		t.Fatal("ambiguous historical cargo was attributed")
	}
	if view.Shipments[0].ShippedQty != "0" {
		t.Fatal("failed projection partially changed response")
	}
	view = progressFixture()
	if mergeContractDispatchProgress(view, []*shippingv1.ContractDispatchLine{{ContractItemId: 11, Quantity: "-10"}}) {
		t.Fatal("negative dispatch was accepted")
	}
}
