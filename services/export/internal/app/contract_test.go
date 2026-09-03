package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

func TestSignContractRejectsConditionsThatNeedUpdateBeforeActivation(t *testing.T) {
	service := &Service{}
	_, err := service.SignContract(context.Background(), 1, 7, "NEEDS_UPDATE", "2026-09-01T13:00:00Z", "price expired", Operator{})
	if got := apierr.CodeFromError(err); got != "EX_CONTRACT_CONDITIONS_NEED_UPDATE" {
		t.Fatalf("code = %q, want EX_CONTRACT_CONDITIONS_NEED_UPDATE (err=%v)", got, err)
	}
}

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

func TestEffectiveEventOnlyRequestsUnprocuredOpeningBalance(t *testing.T) {
	v := view("EXECUTING", "APPROVED", "2026-09-30", 0)
	v.Items = []store.ListContractItemsRow{{
		ID: 11, LineNo: 1, ProductID: 5, ProductName: "冷轧钢卷",
		Qty: "100", OpeningProcuredQty: "65", UomID: 1, UomCode: "TON",
		UnitPrice: "5", Amount: "500.00",
	}}
	e := effectiveEvent(v)
	if got := e.Items[0].RequiredQty; got != "35" {
		t.Fatalf("required qty = %s, want 35", got)
	}
}

func TestCarryOpeningSnapshotPreservesHistory(t *testing.T) {
	old := []store.ListContractItemsRow{{
		LineNo: 1, ProductID: 5, Spec: "1mm", Qty: "100",
		OpeningProcuredQty: "65", OpeningArrivedQty: "40", OpeningShippedQty: "20",
	}}
	lines, err := carryOpeningSnapshot(old, []store.ListContractItemsRow{{
		LineNo: 1, ProductID: 5, Spec: "1mm", Qty: "120",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].OpeningProcuredQty != "65" || lines[0].OpeningArrivedQty != "40" || lines[0].OpeningShippedQty != "20" {
		t.Fatalf("opening history was not preserved: %+v", lines[0])
	}
}

func TestCarryOpeningSnapshotRejectsRewritingHistory(t *testing.T) {
	old := []store.ListContractItemsRow{{
		LineNo: 1, ProductID: 5, Spec: "1mm", Qty: "100", OpeningShippedQty: "20",
	}}
	if _, err := carryOpeningSnapshot(old, []store.ListContractItemsRow{{
		LineNo: 1, ProductID: 5, Spec: "1mm", Qty: "10",
	}}); apierr.CodeFromError(err) != "EX_CHANGE_BELOW_OPENING" {
		t.Fatalf("wrong error for quantity below history: %v", err)
	}
	if _, err := carryOpeningSnapshot(old, nil); apierr.CodeFromError(err) != "EX_CHANGE_REMOVES_OPENING_LINE" {
		t.Fatalf("wrong error for removing historical line: %v", err)
	}
}

func TestPriceExistingLinesAcceptsManualProductSnapshot(t *testing.T) {
	svc := &Service{}
	lines, total, err := svc.priceExistingLines(context.Background(), []ItemInput{{
		ProductName: "  Contract-only alloy  ", UomCode: " ton ", Spec: "A-17",
		Qty: "12.5", UnitPrice: "8",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].product.ID != 0 {
		t.Fatalf("manual snapshot was not retained: %+v", lines)
	}
	if lines[0].product.Name != "Contract-only alloy" || lines[0].product.UomCode != "TON" {
		t.Fatalf("manual snapshot was not normalized: %+v", lines[0].product)
	}
	if total.StringFixed(2) != "100.00" {
		t.Fatalf("total = %s, want 100.00", total)
	}
}

func TestPriceExistingLinesRequiresManualProductUnit(t *testing.T) {
	svc := &Service{}
	_, _, err := svc.priceExistingLines(context.Background(), []ItemInput{{
		ProductName: "Contract-only alloy", Qty: "1", UnitPrice: "1",
	}})
	if apierr.CodeFromError(err) != "EX_UOM_REQUIRED" {
		t.Fatalf("error = %v, want EX_UOM_REQUIRED", err)
	}
}

func TestOpeningDecimalBlankIsNotAnError(t *testing.T) {
	value, err := openingDecimal("", "EX_OPENING_RECEIVED_INVALID", "已收款金额")
	if err != nil {
		t.Fatalf("blank opening amount returned an error: %v", err)
	}
	if !value.IsZero() {
		t.Fatalf("blank opening amount = %s, want zero", value)
	}
}

func TestCarryOpeningSnapshotDistinguishesManualProducts(t *testing.T) {
	old := []store.ListContractItemsRow{{
		LineNo: 1, ProductID: 0, ProductName: "Alloy A", UomCode: "TON", Spec: "1mm",
		Qty: "10", OpeningShippedQty: "2",
	}}
	_, err := carryOpeningSnapshot(old, []store.ListContractItemsRow{{
		LineNo: 1, ProductID: 0, ProductName: "Alloy B", UomCode: "TON", Spec: "1mm", Qty: "10",
	}})
	if apierr.CodeFromError(err) != "EX_CHANGE_REMOVES_OPENING_LINE" {
		t.Fatalf("error = %v, want EX_CHANGE_REMOVES_OPENING_LINE", err)
	}
}
