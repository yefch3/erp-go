package app

import (
	"encoding/json"
	"testing"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

func view(contractStatus, versionStatus, deliveryDate string, lines int) ContractView {
	items := make([]store.ListContractItemsRow, lines)
	return ContractView{
		Contract: store.GetContractRow{ID: 7, ContractNo: "CT-202607-0001", Status: contractStatus},
		Version: store.GetContractVersionRow{
			ID: 3, VersionNo: 1, Status: versionStatus,
			DeliveryDate: deliveryDate, Currency: "EUR", TotalAmount: "6450.00",
		},
		Items: items,
	}
}

func TestReadyToSubmitAcceptsAWorkableDraft(t *testing.T) {
	for _, contractStatus := range []string{"DRAFT", "EFFECTIVE", "EXECUTING"} {
		if err := readyToSubmit(view(contractStatus, "DRAFT", "2026-09-30", 2)); err != nil {
			t.Errorf("contract in %s should be submittable, got %v", contractStatus, err)
		}
	}
}

func TestReadyToSubmitRefusesIncompleteOrBusyContracts(t *testing.T) {
	cases := []struct {
		name string
		in   ContractView
		code string
	}{
		{"version already submitted", view("PENDING_APPROVAL", "PENDING_APPROVAL", "2026-09-30", 2), "EX_CONTRACT_NOT_DRAFT"},
		{"version already approved", view("PENDING_SIGN", "APPROVED", "2026-09-30", 2), "EX_CONTRACT_NOT_DRAFT"},
		{"contract cancelled", view("CANCELLED", "DRAFT", "2026-09-30", 2), "EX_CONTRACT_STATUS_TRANSITION"},
		{"no delivery date", view("DRAFT", "DRAFT", "", 2), "EX_DELIVERY_DATE_REQUIRED"},
		{"no lines", view("DRAFT", "DRAFT", "2026-09-30", 0), "EX_ITEMS_REQUIRED"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := readyToSubmit(c.in)
			if err == nil {
				t.Fatalf("expected %s, got no error", c.code)
			}
			if got := apierr.CodeFromError(err); got != c.code {
				t.Fatalf("expected %s, got %s", c.code, got)
			}
		})
	}
}

// The summary is all an approver sees before deciding, so amount and currency
// must arrive as one readable value rather than two fields to reassemble.
func TestSummaryCarriesAmountWithItsCurrency(t *testing.T) {
	var got map[string]any
	if err := json.Unmarshal([]byte(summaryJSON(view("DRAFT", "DRAFT", "2026-09-30", 1))), &got); err != nil {
		t.Fatal(err)
	}
	if got["amount"] != "6450.00 EUR" {
		t.Errorf("amount = %v, want \"6450.00 EUR\"", got["amount"])
	}
	if _, present := got["version_no"]; present {
		t.Error("version 1 should not clutter the summary with a version number")
	}
	if _, present := got["change_reason"]; present {
		t.Error("a first version has no change reason to show")
	}
}

func TestSummaryExplainsAChange(t *testing.T) {
	v := view("EFFECTIVE", "DRAFT", "2026-10-15", 1)
	v.Version.VersionNo = 2
	v.Version.ChangeReason = "客户追加 300 件"

	var got map[string]any
	if err := json.Unmarshal([]byte(summaryJSON(v)), &got); err != nil {
		t.Fatal(err)
	}
	if got["version_no"] != float64(2) {
		t.Errorf("version_no = %v, want 2", got["version_no"])
	}
	if got["change_reason"] != "客户追加 300 件" {
		t.Errorf("change_reason = %v", got["change_reason"])
	}
}

// Consumers act on this payload without calling back, so every line has to be
// in it, quantities included.
func TestEffectiveEventCarriesTheLines(t *testing.T) {
	sku := int64(42)
	v := view("PENDING_SIGN", "APPROVED", "2026-09-30", 0)
	v.Items = []store.ListContractItemsRow{
		{ID: 11, LineNo: 1, ProductID: 5, SkuID: &sku, ProductCode: "P-00001",
			Qty: "1200.0000", UomID: 1, UomCode: "PCS", UnitPrice: "4.3500", Amount: "5220.00"},
		{ID: 12, LineNo: 2, ProductID: 6, ProductCode: "P-00002",
			Qty: "300.0000", UomID: 1, UomCode: "PCS", UnitPrice: "4.1000", Amount: "1230.00"},
	}

	e := effectiveEvent(v)
	if len(e.Items) != 2 {
		t.Fatalf("got %d lines, want 2", len(e.Items))
	}
	if e.Items[0].SkuID != 42 {
		t.Errorf("sku = %d, want 42", e.Items[0].SkuID)
	}
	// A line without a SKU must report zero, not dereference a nil pointer.
	if e.Items[1].SkuID != 0 {
		t.Errorf("missing sku should be 0, got %d", e.Items[1].SkuID)
	}
	if e.ContractNo != "CT-202607-0001" || e.TotalAmount != "6450.00" {
		t.Errorf("header not copied: %+v", e)
	}
}
