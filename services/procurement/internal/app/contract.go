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
// Normal contracts raise their full quantity. An imported existing contract
// can carry a required_qty balance so work completed before ERP takeover is
// not generated again.
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
	SalesEmployeeID       int64          `json:"sales_employee_id"`
	SalesEmployee         string         `json:"sales_employee"`
	Currency              string         `json:"currency"`
	PortOfDischarge       string         `json:"port_of_discharge"`
	ExistingContract      bool           `json:"existing_contract"`
	ProcurementEmployeeID int64          `json:"procurement_employee_id"`
	ProcurementEmployee   string         `json:"procurement_employee"`
	SupplierID            int64          `json:"supplier_id"`
	Items                 []ContractLine `json:"items"`
}

type ContractLine struct {
	LineNo            int32  `json:"line_no"`
	ItemID            int64  `json:"contract_item_id"`
	ProductID         int64  `json:"product_id"`
	SkuID             int64  `json:"sku_id"`
	ProductCode       string `json:"product_code"`
	ProductName       string `json:"product_name"`
	Qty               string `json:"qty"`
	RequiredQty       string `json:"required_qty"`
	UomID             int64  `json:"uom_id"`
	UomCode           string `json:"uom_code"`
	Spec              string `json:"spec"`
	PurchaseUnitPrice string `json:"purchase_unit_price"`
	OpeningArrivedQty string `json:"opening_arrived_qty"`
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
	if e.ExistingContract {
		return s.importExistingContractOrder(ctx, tenantID, e, log, claim)
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
			requiredQty := procurementRequiredQty(line)
			qty, err := decimal.NewFromString(requiredQty)
			if err != nil || qty.LessThanOrEqual(decimal.Zero) {
				// Zero is a valid opening position: procurement was already fully
				// arranged before the contract entered this ERP.
				if err != nil || qty.IsNegative() {
					log.Warn("contract line with unusable required quantity, skipped",
						"contract_no", e.ContractNo, "item", line.ItemID, "qty", requiredQty)
				}
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
				if err := applyContractProcurementSnapshot(e, line, line.Qty, snapshots[index], &params); err != nil {
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

// importExistingContractOrder reconstructs history, it does not start a new
// approval workflow. The paper contract was already signed and its purchase
// order was already placed before this ERP took over, so the first system
// record is an ORDERED/PARTIALLY_RECEIVED/RECEIVED order owned by the original
// buyer. Opening receipts are audit records only; inventory opening balances
// remain a separate takeover concern and are not posted twice here.
func (s *Service) importExistingContractOrder(ctx context.Context, tenantID int64, e ContractEffective, log *slog.Logger, claim EventClaim) error {
	if e.ProcurementEmployeeID == 0 || e.SupplierID == 0 {
		return fmt.Errorf("existing contract %s is missing original buyer or supplier", e.ContractNo)
	}
	supplier, err := s.supplierForOrder(ctx, e.SupplierID)
	if err != nil {
		return err
	}
	poNo, err := s.numbering.Next(ctx, "PURCHASE_ORDER")
	if err != nil {
		return err
	}
	hasArrival := false
	for _, line := range e.Items {
		arrived, parseErr := decimal.NewFromString(orZero(line.OpeningArrivedQty))
		if parseErr != nil || arrived.IsNegative() {
			return fmt.Errorf("existing contract %s line %d has invalid opening arrival", e.ContractNo, line.LineNo)
		}
		hasArrival = hasArrival || arrived.IsPositive()
	}
	receiptNo := ""
	if hasArrival {
		receiptNo, err = s.numbering.Next(ctx, "INBOUND")
		if err != nil {
			return err
		}
	}

	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := claim(ctx, tx); err != nil {
			return err
		}
		q := s.q.WithTx(tx)
		total := decimal.Zero
		for _, line := range e.Items {
			qty, qtyErr := decimal.NewFromString(line.Qty)
			price, priceErr := decimal.NewFromString(orZero(line.PurchaseUnitPrice))
			if qtyErr != nil || qty.LessThanOrEqual(decimal.Zero) || priceErr != nil || price.IsNegative() {
				return fmt.Errorf("existing contract %s line %d has invalid purchase quantity or price", e.ContractNo, line.LineNo)
			}
			total = total.Add(qty.Mul(price))
		}
		head, createErr := q.CreatePurchaseOrder(ctx, store.CreatePurchaseOrderParams{
			TenantID: tenantID, PoNo: poNo, SupplierID: supplier.ID,
			SupplierCode: supplier.Code, SupplierName: supplier.Name,
			Currency: orDefaultCurrency(e.Currency), TotalAmount: total.StringFixed(2),
			ExpectedDate: e.DeliveryDate, BuyerID: e.ProcurementEmployeeID, BuyerName: e.ProcurementEmployee,
			Remark:          "手工录入已有合同 " + e.ContractNo,
			FulfillmentMode: "DIRECT_SHIP", DeliveryLocationType: "PORT",
			DeliveryPortName: e.PortOfDischarge,
		})
		if createErr != nil {
			return createErr
		}
		type arrivedItem struct {
			id  int64
			qty string
		}
		arrivals := make([]arrivedItem, 0, len(e.Items))
		allReceived, anyReceived := true, false
		for _, line := range e.Items {
			qty := decimal.RequireFromString(line.Qty)
			price := decimal.RequireFromString(orZero(line.PurchaseUnitPrice))
			arrived := decimal.RequireFromString(orZero(line.OpeningArrivedQty))
			if arrived.GreaterThan(qty) {
				return fmt.Errorf("existing contract %s line %d arrival exceeds ordered quantity", e.ContractNo, line.LineNo)
			}
			requirementID, reqErr := q.UpsertRequirement(ctx, store.UpsertRequirementParams{
				TenantID: tenantID, ContractID: e.ContractID, ContractNo: e.ContractNo,
				ContractVersionID: e.VersionID, VersionNo: e.VersionNo, ContractItemID: line.ItemID,
				CustomerName: e.CustomerName, ProductID: line.ProductID, SkuID: line.SkuID,
				ProductCode: line.ProductCode, ProductName: line.ProductName, Spec: line.Spec,
				UomID: line.UomID, UomCode: line.UomCode, RequiredQty: qty.String(),
				RequiredDate: e.DeliveryDate, Source: "CONTRACT",
				OwnerID: e.ProcurementEmployeeID, OwnerName: e.ProcurementEmployee,
				SupplierID: supplier.ID, SupplierName: supplier.Name,
				SourceCurrency: orDefaultCurrency(e.Currency), SourceUnitPrice: price.String(),
			})
			if reqErr != nil {
				return reqErr
			}
			itemID, itemErr := q.CreatePurchaseOrderItem(ctx, store.CreatePurchaseOrderItemParams{
				TenantID: tenantID, PoID: head.ID, RequirementID: requirementID,
				ProductID: line.ProductID, SkuID: line.SkuID, ProductCode: line.ProductCode,
				ProductName: line.ProductName, Spec: line.Spec, UomID: line.UomID, UomCode: line.UomCode,
				Qty: qty.String(), UnitPrice: price.String(), Amount: qty.Mul(price).StringFixed(2),
			})
			if itemErr != nil {
				return itemErr
			}
			if _, reqErr = q.AddRequirementOrdered(ctx, store.AddRequirementOrderedParams{TenantID: tenantID, ID: requirementID, Qty: qty.String()}); reqErr != nil {
				return reqErr
			}
			if arrived.IsPositive() {
				anyReceived = true
				if err := q.AddPurchaseOrderItemReceived(ctx, store.AddPurchaseOrderItemReceivedParams{TenantID: tenantID, ID: itemID, Qty: arrived.String()}); err != nil {
					return err
				}
				if _, err := q.AddRequirementReceived(ctx, store.AddRequirementReceivedParams{TenantID: tenantID, ID: requirementID, Qty: arrived.String()}); err != nil {
					return err
				}
				arrivals = append(arrivals, arrivedItem{id: itemID, qty: arrived.String()})
			}
			allReceived = allReceived && arrived.Equal(qty)
		}
		if err := q.SetPurchaseOrderOrdered(ctx, store.SetPurchaseOrderOrderedParams{TenantID: tenantID, ID: head.ID}); err != nil {
			return err
		}
		status := existingContractOrderStatus(anyReceived, allReceived)
		if status != poOrdered {
			if err := q.SetPurchaseOrderStatus(ctx, store.SetPurchaseOrderStatusParams{TenantID: tenantID, ID: head.ID, NewStatus: status}); err != nil {
				return err
			}
		}
		if len(arrivals) > 0 {
			receipt, receiptErr := q.CreatePurchaseReceipt(ctx, store.CreatePurchaseReceiptParams{
				TenantID: tenantID, PoID: head.ID, ReceiptNo: receiptNo,
				OperatorID: e.ProcurementEmployeeID, OperatorName: e.ProcurementEmployee,
				Remark: "系统接管时自动补录的历史到货",
			})
			if receiptErr != nil {
				return receiptErr
			}
			for _, item := range arrivals {
				if err := q.CreatePurchaseReceiptItem(ctx, store.CreatePurchaseReceiptItemParams{TenantID: tenantID, ReceiptID: receipt.ID, PoItemID: item.id, Qty: item.qty}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.nudge(ctx, tenantID)
	log.Info("existing contract reconstructed as ordered purchase order", "contract_no", e.ContractNo, "po_no", poNo)
	return nil
}

func existingContractOrderStatus(anyReceived, allReceived bool) string {
	if allReceived {
		return poReceived
	}
	if anyReceived {
		return poPartial
	}
	return poOrdered
}

func orDefaultCurrency(currency string) string {
	if currency == "" {
		return "CNY"
	}
	return currency
}

func procurementRequiredQty(line ContractLine) string {
	if line.RequiredQty != "" {
		return line.RequiredQty
	}
	return line.Qty
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
