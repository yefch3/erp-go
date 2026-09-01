package app

import (
	"testing"

	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

func TestApplyContractProcurementSnapshotAssignsOriginalBuyerAndFinalTerms(t *testing.T) {
	event := ContractEffective{ContractNo: "CT-1", QuotationID: 91, QuotationNo: "QT-1"}
	line := ContractLine{LineNo: 1, ProductName: "冷轧钢卷", UomCode: "TON"}
	snapshot := store.ListContractProcurementSnapshotsRow{
		SelectionItemID: 7, ProductName: "冷轧钢卷", ConfirmedQty: "10", UomCode: "TON",
		CaseID: 8, SourcingLineID: 9, SupplierQuoteLineID: 10,
		BuyerID: 23, BuyerName: "T2 采购专员", SupplierID: 11, SupplierName: "测试004",
		FactoryID: 12, FactoryName: "工厂 A", FinalCurrency: "USD", FinalUnitPrice: "5.000000",
		Moq: "4", FinalLeadTime: 5, FinalDeliveryDate: "2026-09-12",
		FinalPaymentTerms: "T/T", FinalIncoterm: "EXW", FinalValidUntil: "2026-09-10",
	}
	params := store.UpsertRequirementParams{}
	if err := applyContractProcurementSnapshot(event, line, "10", snapshot, &params); err != nil {
		t.Fatal(err)
	}
	if params.OwnerID != 23 || params.SupplierID != 11 || params.SourceUnitPrice != "5.000000" || params.SourcePaymentTerms != "T/T" || params.SourceIncoterm != "EXW" {
		t.Fatalf("wrong execution task snapshot: %+v", params)
	}
}

func TestApplyContractProcurementSnapshotAcceptsEquivalentDecimalScales(t *testing.T) {
	event := ContractEffective{ContractNo: "CT-1"}
	line := ContractLine{LineNo: 1, ProductName: "冷轧钢卷", UomCode: "TON"}
	snapshot := store.ListContractProcurementSnapshotsRow{
		SelectionItemID: 7, ProductName: "冷轧钢卷", ConfirmedQty: "200.0000", UomCode: "TON",
	}

	if err := applyContractProcurementSnapshot(event, line, "200", snapshot, &store.UpsertRequirementParams{}); err != nil {
		t.Fatalf("equivalent decimal quantities must match: %v", err)
	}
}

func TestApplyContractProcurementSnapshotRejectsNumericallyDifferentQuantity(t *testing.T) {
	err := applyContractProcurementSnapshot(
		ContractEffective{ContractNo: "CT-1"}, ContractLine{LineNo: 1, ProductName: "冷轧钢卷", UomCode: "TON"}, "200.0001",
		store.ListContractProcurementSnapshotsRow{SelectionItemID: 7, ProductName: "冷轧钢卷", ConfirmedQty: "200.0000", UomCode: "TON"},
		&store.UpsertRequirementParams{},
	)
	if err == nil {
		t.Fatal("expected numerically different quantities to be rejected")
	}
}

func TestApplyContractProcurementSnapshotRejectsMismatchedLine(t *testing.T) {
	err := applyContractProcurementSnapshot(
		ContractEffective{ContractNo: "CT-1"}, ContractLine{LineNo: 1, ProductName: "A", UomCode: "TON"}, "10",
		store.ListContractProcurementSnapshotsRow{SelectionItemID: 7, ProductName: "B", ConfirmedQty: "10", UomCode: "TON"},
		&store.UpsertRequirementParams{},
	)
	if err == nil {
		t.Fatal("expected mismatch to be rejected")
	}
}
