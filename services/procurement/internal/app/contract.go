package app

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

// ContractEffective is the procurement-relevant snapshot emitted by export.
//
// This company buys against confirmed customer business and does not fulfil a
// sale from its own stock. Procurement therefore consumes the contract
// directly and raises its FULL quantity. The inventory service may still keep
// a custody ledger for goods that have reached a port terminal, but that
// ledger must never reduce what is sent to the mill.
type ContractEffective struct {
	ContractID                int64  `json:"contract_id"`
	ContractNo                string `json:"contract_no"`
	VersionID                 int64  `json:"version_id"`
	VersionNo                 int32  `json:"version_no"`
	CustomerName              string `json:"customer_name"`
	DeliveryDate              string `json:"delivery_date"`
	QuotationID               int64  `json:"quotation_id"`
	QuotationNo               string `json:"quotation_no"`
	SourceCustomerSelectionID int64  `json:"source_customer_selection_id"`
	// 合同负责人：拆出的需求生而继承它作为属主（A1）。旧事件不带这
	// 两个字段时归 0——属主未知，只有「全部」范围能看见。
	SalesEmployeeID int64          `json:"sales_employee_id"`
	SalesEmployee   string         `json:"sales_employee"`
	Items           []ContractLine `json:"items"`
}

type ContractLine struct {
	LineNo      int32  `json:"line_no"`
	ItemID      int64  `json:"contract_item_id"`
	ProductID   int64  `json:"product_id"`
	SkuID       int64  `json:"sku_id"`
	ProductCode string `json:"product_code"`
	ProductName string `json:"product_name"`
	Qty         string `json:"qty"`
	UomID       int64  `json:"uom_id"`
	UomCode     string `json:"uom_code"`
}

// RequirementsFromContract creates one gross purchase requirement for each
// effective contract line. A redelivered event refreshes the same line; a new
// contract version supersedes untouched requirements from the old version.
func (s *Service) RequirementsFromContract(ctx context.Context, tenantID int64, e ContractEffective, log *slog.Logger, claim EventClaim) error {
	if len(e.Items) == 0 {
		log.Warn("contract effective event has no lines, nothing to source",
			"contract_id", e.ContractID, "contract_no", e.ContractNo)
		return nil
	}

	var snapshots []store.ListContractProcurementSnapshotsRow
	if e.SourceCustomerSelectionID != 0 {
		var err error
		snapshots, err = s.q.ListContractProcurementSnapshots(ctx, store.ListContractProcurementSnapshotsParams{TenantID: tenantID, SelectionID: e.SourceCustomerSelectionID})
		if err != nil {
			return err
		}
		if len(snapshots) != len(e.Items) {
			return fmt.Errorf("contract %s selection %d has %d procurement snapshots for %d lines", e.ContractNo, e.SourceCustomerSelectionID, len(snapshots), len(e.Items))
		}
	}
	var superseded []store.SupersedeRequirementsBeforeRow
	var strandedOrders int64
	sourced := 0
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		// 认领与这一笔业务写入同生共死：崩溃一起回滚，提交一起落库。
		// 见 eventclaim.go。
		if err := claim(ctx, tx); err != nil {
			return err
		}
		q := s.q.WithTx(tx)
		for index, line := range e.Items {
			qty, err := decimal.NewFromString(line.Qty)
			if err != nil || qty.LessThanOrEqual(decimal.Zero) {
				log.Warn("contract line with unusable quantity, skipped",
					"contract_no", e.ContractNo, "item", line.ItemID, "qty", line.Qty)
				continue
			}
			params := store.UpsertRequirementParams{
				TenantID: tenantID, ContractID: e.ContractID, ContractNo: e.ContractNo,
				ContractVersionID: e.VersionID, VersionNo: e.VersionNo,
				ContractItemID: line.ItemID, CustomerName: e.CustomerName,
				ProductID: line.ProductID, SkuID: line.SkuID,
				ProductCode: line.ProductCode, ProductName: line.ProductName,
				UomID: line.UomID, UomCode: line.UomCode,
				RequiredQty: qty.String(), RequiredDate: e.DeliveryDate, Source: "CONTRACT",
				OwnerID: e.SalesEmployeeID, OwnerName: e.SalesEmployee,
				SourceUnitPrice: "0",
			}
			if len(snapshots) > 0 {
				if err := applyContractProcurementSnapshot(e, line, qty.String(), snapshots[index], &params); err != nil {
					return err
				}
			}
			if _, err := q.UpsertRequirement(ctx, params); err != nil {
				return err
			}
			sourced++
		}

		var err error
		superseded, err = q.SupersedeRequirementsBefore(ctx, store.SupersedeRequirementsBeforeParams{
			TenantID: tenantID, ContractID: e.ContractID, KeepVersionID: e.VersionID,
			Reason: "合同变更，已由新版本的全量采购需求取代",
		})
		if err != nil {
			return err
		}
		strandedOrders, err = q.CountOrderedOnOldVersions(ctx, store.CountOrderedOnOldVersionsParams{
			TenantID: tenantID, ContractID: e.ContractID, KeepVersionID: e.VersionID,
		})
		return err
	})
	if err != nil {
		return err
	}

	s.nudge(ctx, tenantID)
	log.Info("full purchase requirements sourced from effective contract",
		"contract_id", e.ContractID, "contract_no", e.ContractNo,
		"version_no", e.VersionNo, "lines", sourced, "superseded", len(superseded))
	if strandedOrders > 0 {
		log.Warn("contract changed after purchase orders were raised against it",
			"contract_id", e.ContractID, "contract_no", e.ContractNo,
			"ordered_requirements_on_old_versions", strandedOrders)
	}
	return nil
}

func applyContractProcurementSnapshot(e ContractEffective, line ContractLine, qty string, snapshot store.ListContractProcurementSnapshotsRow, params *store.UpsertRequirementParams) error {
	confirmedQty, confirmedQtyErr := decimal.NewFromString(snapshot.ConfirmedQty)
	contractQty, contractQtyErr := decimal.NewFromString(qty)
	quantityMatches := confirmedQtyErr == nil && contractQtyErr == nil && confirmedQty.Equal(contractQty)
	if snapshot.ProductName != line.ProductName || !quantityMatches || snapshot.UomCode != line.UomCode {
		return fmt.Errorf("contract %s line %d does not match frozen customer selection item %d", e.ContractNo, line.LineNo, snapshot.SelectionItemID)
	}
	params.Source = "CUSTOMER_QUOTATION"
	params.Spec = snapshot.ProductSpec
	params.RequiredDate = snapshot.FinalDeliveryDate
	params.OwnerID, params.OwnerName = snapshot.BuyerID, snapshot.BuyerName
	params.QuotationID, params.QuotationNo = e.QuotationID, e.QuotationNo
	params.SourcingCaseID, params.SourcingLineID = snapshot.CaseID, snapshot.SourcingLineID
	params.SupplierQuoteLineID = snapshot.SupplierQuoteLineID
	params.SupplierID, params.SupplierName = snapshot.SupplierID, snapshot.SupplierName
	params.FactoryID, params.FactoryName = snapshot.FactoryID, snapshot.FactoryName
	params.SourceCurrency, params.SourceUnitPrice = snapshot.FinalCurrency, snapshot.FinalUnitPrice
	params.Moq, params.LeadTime = snapshot.Moq, strconv.Itoa(int(snapshot.FinalLeadTime))+" 天"
	params.SourcePaymentTerms, params.SourceIncoterm = snapshot.FinalPaymentTerms, snapshot.FinalIncoterm
	params.SourceValidUntil = snapshot.FinalValidUntil
	return nil
}
