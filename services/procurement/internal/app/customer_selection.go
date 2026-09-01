package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

type ConfirmCustomerSelectionInput struct {
	CaseID, SalesPlanID               int64
	SalesPlanItemIDs                  []int64
	ItemChoices                       []CustomerSelectionItemChoiceInput
	CustomerContact, ConfirmationNote string
	CustomerConfirmedAt               string
	ShipmentChoices                   []CustomerShipmentChoiceInput
}

type CustomerSelectionItemChoiceInput struct {
	SalesPlanItemID int64
	ConfirmedQty    string
}

type CustomerShipmentChoiceInput struct {
	ShipmentGroupKey      string
	SalesShippingOptionID int64
	CustomerManaged       bool
}

type FinalCustomerItemPriceInput struct {
	SelectionItemID                             int64
	Currency, UnitPrice, PaymentTerms, Incoterm string
	RequiredDate                                string
}
type FinalCustomerShipmentPriceInput struct {
	SelectionShipmentID     int64
	Currency, FreightAmount string
}
type DecideCustomerSelectionInput struct {
	CaseID, SelectionID                      int64
	Accepted                                 bool
	CustomerContact, DecisionNote, DecidedAt string
	ItemPrices                               []FinalCustomerItemPriceInput
	ShipmentPrices                           []FinalCustomerShipmentPriceInput
}

type CustomerSelectionShipmentView struct {
	Header           store.ListCustomerSelectionShipmentsRow
	SelectionItemIDs []int64
}

type CustomerSelectionView struct {
	Header    store.ListCustomerSelectionsRow
	Items     []store.ListCustomerSelectionItemsRow
	Shipments []CustomerSelectionShipmentView
	Tasks     []store.ListFinalRecheckTasksRow
}

func customerShipmentGroupKey(supplierID, factoryID int64) string {
	return fmt.Sprintf("SUPPLIER:%d:FACTORY:%d", supplierID, factoryID)
}

func (s *Service) ConfirmCustomerSelection(ctx context.Context, tenantID int64, in ConfirmCustomerSelectionInput, op Operator) (CustomerSelectionView, error) {
	in.CustomerContact = strings.TrimSpace(in.CustomerContact)
	in.ConfirmationNote = strings.TrimSpace(in.ConfirmationNote)
	in.CustomerConfirmedAt = strings.TrimSpace(in.CustomerConfirmedAt)
	if in.CaseID == 0 || in.SalesPlanID == 0 || (len(in.SalesPlanItemIDs) == 0 && len(in.ItemChoices) == 0) || in.CustomerConfirmedAt == "" {
		return CustomerSelectionView{}, apierr.Invalid("SC_CUSTOMER_SELECTION_REQUIRED", "请选择客户意向产品方案、填写意向数量和沟通时间")
	}
	if _, err := time.Parse(time.RFC3339, in.CustomerConfirmedAt); err != nil {
		return CustomerSelectionView{}, apierr.Invalid("SC_CUSTOMER_SELECTION_TIME", "客户确认时间格式无效")
	}
	caseRow, err := s.requireResponsibleSales(ctx, tenantID, in.CaseID, op)
	if err != nil {
		return CustomerSelectionView{}, err
	}
	plan, err := s.q.GetSalesPlan(ctx, store.GetSalesPlanParams{TenantID: tenantID, ID: in.SalesPlanID})
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (plan.CaseID != in.CaseID || plan.Status != "PRESENTED" || plan.RequirementVersionNo != caseRow.RequirementVersionNo) {
		return CustomerSelectionView{}, apierr.Conflict("SC_CUSTOMER_SELECTION_PLAN_STALE", "客户沟通方案已失效，请基于当前需求重新生成方案")
	}
	if err != nil {
		return CustomerSelectionView{}, err
	}

	seenItems, seenLines := map[int64]bool{}, map[int64]bool{}
	var selectionID int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		choices := in.ItemChoices
		if len(choices) == 0 {
			choices = make([]CustomerSelectionItemChoiceInput, 0, len(in.SalesPlanItemIDs))
			for _, itemID := range in.SalesPlanItemIDs {
				choices = append(choices, CustomerSelectionItemChoiceInput{SalesPlanItemID: itemID})
			}
		}
		candidates := make([]store.CustomerSelectionCandidateRow, 0, len(choices))
		confirmedQtyByItem := make(map[int64]string, len(choices))
		batchLines := map[string]map[int64]bool{}
		selectedByLine := map[int64]store.CustomerSelectionCandidateRow{}
		for _, choice := range choices {
			itemID := choice.SalesPlanItemID
			if itemID == 0 || seenItems[itemID] {
				return apierr.Invalid("SC_CUSTOMER_SELECTION_DUPLICATE", "客户选择中存在重复方案")
			}
			candidate, candidateErr := q.CustomerSelectionCandidate(ctx, store.CustomerSelectionCandidateParams{TenantID: tenantID, CaseID: in.CaseID, ID: in.SalesPlanID, ID_2: itemID})
			if candidateErr != nil {
				return apierr.Conflict("SC_CUSTOMER_SELECTION_ITEM_STALE", "所选产品方案已失效或超过有效期")
			}
			if seenLines[candidate.SourcingLineID] {
				return apierr.Invalid("SC_CUSTOMER_SELECTION_ONE_PER_PRODUCT", "同一产品只能确认一个最终方案")
			}
			confirmedQty := strings.TrimSpace(choice.ConfirmedQty)
			if confirmedQty == "" {
				confirmedQty = candidate.SpiQuotedQty
			}
			quantity, quantityErr := decimal.NewFromString(confirmedQty)
			available, availableErr := decimal.NewFromString(candidate.AvailableQty)
			if quantityErr != nil || !quantity.IsPositive() {
				return apierr.Invalid("SC_CUSTOMER_SELECTION_QUANTITY", "客户意向数量必须大于 0")
			}
			if availableErr != nil || quantity.GreaterThan(available) {
				return apierr.Invalid("SC_CUSTOMER_SELECTION_QUANTITY_EXCEEDS_AVAILABLE", "客户意向数量不能超过供应商可供数量")
			}
			confirmedQtyByItem[candidate.SalesPlanItemID] = quantity.String()
			seenItems[itemID], seenLines[candidate.SourcingLineID] = true, true
			candidates = append(candidates, candidate)
			selectedByLine[candidate.SourcingLineID] = candidate
			groupKey := customerShipmentGroupKey(candidate.SupplierID, candidate.FactoryID)
			if batchLines[groupKey] == nil {
				batchLines[groupKey] = map[int64]bool{}
			}
			batchLines[groupKey][candidate.SourcingLineID] = true
		}
		shipmentChoices := map[string]store.CustomerShipmentCandidateRow{}
		customerManagedGroups := map[string]bool{}
		shipmentChoiceLines := map[string][]store.CustomerShipmentCandidateLinesRow{}
		seenShippingOption := map[int64]bool{}
		for _, choice := range in.ShipmentChoices {
			choice.ShipmentGroupKey = strings.TrimSpace(choice.ShipmentGroupKey)
			requiredLines := batchLines[choice.ShipmentGroupKey]
			if choice.ShipmentGroupKey == "" || len(requiredLines) == 0 {
				return apierr.Invalid("SC_CUSTOMER_SHIPMENT_GROUP", "船运选择不属于当前已选供应商批次")
			}
			if _, duplicate := shipmentChoices[choice.ShipmentGroupKey]; duplicate || customerManagedGroups[choice.ShipmentGroupKey] {
				return apierr.Invalid("SC_CUSTOMER_SHIPMENT_DUPLICATE", "同一货运批次只能选择一个船运方案")
			}
			if choice.CustomerManaged {
				if choice.SalesShippingOptionID != 0 {
					return apierr.Invalid("SC_CUSTOMER_SHIPMENT_MODE", "客户自理运输不能同时选择船运方案")
				}
				customerManagedGroups[choice.ShipmentGroupKey] = true
				continue
			}
			if choice.SalesShippingOptionID == 0 {
				return apierr.Invalid("SC_CUSTOMER_SHIPMENT_REQUIRED", "每个货运批次必须选择船运方案或明确客户自理运输")
			}
			shipping, shippingErr := q.CustomerShipmentCandidate(ctx, store.CustomerShipmentCandidateParams{TenantID: tenantID, CaseID: in.CaseID, ID: in.SalesPlanID, ID_2: choice.SalesShippingOptionID})
			if shippingErr != nil {
				return apierr.Conflict("SC_CUSTOMER_SHIPMENT_STALE", "所选船运方案已失效或超过有效期")
			}
			if seenShippingOption[shipping.ID] {
				return apierr.Invalid("SC_CUSTOMER_SHIPMENT_REUSED", "不同供应商批次需要分别选择船运方案")
			}
			lines, linesErr := q.CustomerShipmentCandidateLines(ctx, store.CustomerShipmentCandidateLinesParams{TenantID: tenantID, SalesShippingOptionID: shipping.ID})
			if linesErr != nil {
				return linesErr
			}
			covered := map[int64]bool{}
			for _, line := range lines {
				covered[line.SourcingLineID] = true
				if selected, required := selectedByLine[line.SourcingLineID]; required {
					quoted, quotedErr := decimal.NewFromString(line.QuotedQty)
					needed, neededErr := decimal.NewFromString(confirmedQtyByItem[selected.SalesPlanItemID])
					if quotedErr != nil || neededErr != nil || quoted.LessThan(needed) {
						return apierr.Invalid("SC_CUSTOMER_SHIPMENT_QUANTITY", "所选船运方案的承运数量不足")
					}
					if selected.RequiredDestinationPort != "" && shipping.PortOfDischarge != "" && !strings.EqualFold(strings.TrimSpace(selected.RequiredDestinationPort), strings.TrimSpace(shipping.PortOfDischarge)) {
						return apierr.Invalid("SC_CUSTOMER_SHIPMENT_DESTINATION", "所选船运方案的目的港与客户需求不一致")
					}
					if selected.PromisedDeliveryDate != "" && shipping.EstimatedArrival != "" && shipping.EstimatedArrival > selected.PromisedDeliveryDate {
						return apierr.Invalid("SC_CUSTOMER_SHIPMENT_ARRIVAL", "所选船运方案预计到港日晚于对客承诺交期")
					}
				}
			}
			if shipping.EstimatedDeparture != "" && shipping.EstimatedArrival != "" && shipping.EstimatedDeparture > shipping.EstimatedArrival {
				return apierr.Invalid("SC_CUSTOMER_SHIPMENT_SCHEDULE", "所选船运方案的开船日不能晚于到港日")
			}
			for lineID := range requiredLines {
				if !covered[lineID] {
					return apierr.Invalid("SC_CUSTOMER_SHIPMENT_INCOMPATIBLE", "所选船运方案不能覆盖该批次的全部产品")
				}
			}
			seenShippingOption[shipping.ID] = true
			shipmentChoices[choice.ShipmentGroupKey] = shipping
			shipmentChoiceLines[choice.ShipmentGroupKey] = lines
		}
		for groupKey := range batchLines {
			if _, arranged := shipmentChoices[groupKey]; !arranged && !customerManagedGroups[groupKey] {
				return apierr.Invalid("SC_CUSTOMER_SHIPMENT_REQUIRED", "每个货运批次必须选择船运方案或明确客户自理运输")
			}
		}
		if invalidateErr := q.InvalidateActiveCustomerSelections(ctx, store.InvalidateActiveCustomerSelectionsParams{Reason: "销售登记了新的客户意向选择", TenantID: tenantID, CaseID: in.CaseID}); invalidateErr != nil {
			return invalidateErr
		}
		id, createErr := q.CreateCustomerSelection(ctx, store.CreateCustomerSelectionParams{TenantID: tenantID, CaseID: in.CaseID, SalesPlanID: in.SalesPlanID, RequirementVersionNo: caseRow.RequirementVersionNo, CustomerContact: in.CustomerContact, ConfirmationNote: in.ConfirmationNote, CustomerConfirmedAt: in.CustomerConfirmedAt, CreatedBy: op.ID, CreatedByName: op.Name})
		if createErr != nil {
			return createErr
		}
		selectionID = id
		selectionItemIDsByGroup := map[string][]int64{}
		for _, candidate := range candidates {
			groupKey := customerShipmentGroupKey(candidate.SupplierID, candidate.FactoryID)
			selectionItemID, itemErr := q.CreateCustomerSelectionItem(ctx, store.CreateCustomerSelectionItemParams{TenantID: tenantID, SelectionID: selectionID, SalesPlanItemID: candidate.SalesPlanItemID, SourcingLineID: candidate.SourcingLineID, ProcurementPlanItemID: candidate.ProcurementPlanItemID, SupplierQuoteLineID: candidate.SupplierQuoteLineID, ProductName: candidate.ProductName, ProductSpec: candidate.ProductSpec, ConfirmedQty: confirmedQtyByItem[candidate.SalesPlanItemID], UomCode: candidate.UomCode, CustomerCurrency: candidate.CustomerCurrency, CustomerUnitPrice: candidate.SpiCustomerUnitPrice, PromisedDeliveryDate: candidate.PromisedDeliveryDate, LineNote: candidate.LineNote, SupplierID: candidate.SupplierID, SupplierName: candidate.SupplierName, FactoryID: candidate.FactoryID, FactoryName: candidate.FactoryName, ShipmentGroupKey: groupKey, CustomerManagedShipping: customerManagedGroups[groupKey]})
			if itemErr != nil {
				return itemErr
			}
			selectionItemIDsByGroup[groupKey] = append(selectionItemIDsByGroup[groupKey], selectionItemID)
			reason := fmt.Sprintf("客户意向选择了该方案，意向数量 %s %s；请按该数量复核最终价格、可供数量与交期", confirmedQtyByItem[candidate.SalesPlanItemID], candidate.UomCode)
			procurementReworkID, reworkErr := q.CreateProcurementReworkRequest(ctx, store.CreateProcurementReworkRequestParams{TenantID: tenantID, CaseID: in.CaseID, PlanID: plan.ProcurementPlanID, SourcingLineID: candidate.SourcingLineID, SupplierQuoteLineID: candidate.SupplierQuoteLineID, RequestType: "REQUOTE", ScopeType: "QUOTE", AssignedBuyerID: candidate.BuyerID, AssignedBuyerName: candidate.BuyerName, SupplierID: candidate.SupplierID, SupplierName: candidate.SupplierName, ProductName: candidate.ProductName, Reason: reason, CreatedBy: op.ID, CreatedByName: op.Name})
			if reworkErr != nil {
				return reworkErr
			}
			if taskErr := q.CreateFinalProcurementRecheckTask(ctx, store.CreateFinalProcurementRecheckTaskParams{TenantID: tenantID, SelectionID: selectionID, SelectionItemID: &selectionItemID, ProcurementReworkID: &procurementReworkID}); taskErr != nil {
				return taskErr
			}
		}
		for groupKey, shipping := range shipmentChoices {
			selectionShipmentID, shipmentErr := q.CreateCustomerSelectionShipment(ctx, store.CreateCustomerSelectionShipmentParams{TenantID: tenantID, SelectionID: selectionID, ShipmentGroupKey: groupKey, SalesShippingOptionID: shipping.ID, ShippingOptionID: shipping.ShippingOptionID, CarrierForwarder: shipping.CarrierForwarder, ServiceOptionName: shipping.ServiceOptionName, ShippingEmployeeID: shipping.ShippingEmployeeID, ShippingEmployeeName: shipping.ShippingEmployeeName, CustomerCurrency: shipping.CustomerCurrency, CustomerFreightAmount: shipping.SsoCustomerFreightAmount, ChargeBasis: shipping.ChargeBasis, PortOfLoading: shipping.PortOfLoading, PortOfDischarge: shipping.PortOfDischarge, EstimatedDeparture: shipping.EstimatedDeparture, EstimatedArrival: shipping.EstimatedArrival, ValidUntil: shipping.ValidUntil, CustomerNote: shipping.CustomerNote})
			if shipmentErr != nil {
				return shipmentErr
			}
			for _, selectionItemID := range selectionItemIDsByGroup[groupKey] {
				if linkErr := q.LinkCustomerSelectionShipmentItem(ctx, store.LinkCustomerSelectionShipmentItemParams{TenantID: tenantID, SelectionShipmentID: selectionShipmentID, SelectionItemID: selectionItemID}); linkErr != nil {
					return linkErr
				}
			}
			lines := shipmentChoiceLines[groupKey]
			if len(lines) == 0 {
				return apierr.Invalid("SC_CUSTOMER_SHIPMENT_EMPTY", "所选船运方案没有可复询的货物明细")
			}
			firstLine := lines[0]
			for _, line := range lines {
				if _, covered := batchLines[groupKey][line.SourcingLineID]; covered {
					firstLine = line
					break
				}
			}
			productNames := make([]string, 0, len(selectionItemIDsByGroup[groupKey]))
			for _, candidate := range candidates {
				if customerShipmentGroupKey(candidate.SupplierID, candidate.FactoryID) == groupKey {
					productNames = append(productNames, candidate.ProductName)
				}
			}
			shippingReworkID, shippingErr := q.CreateShippingRework(ctx, store.CreateShippingReworkParams{TenantID: tenantID, CaseID: in.CaseID, SalesPlanID: in.SalesPlanID, SourcingLineID: firstLine.SourcingLineID, ShippingOptionLineID: firstLine.ShippingOptionLineID, RequestType: "REQUOTE", ScopeType: "QUOTE", AssignedShippingID: shipping.ShippingEmployeeID, AssignedShippingName: shipping.ShippingEmployeeName, CarrierForwarder: shipping.CarrierForwarder, ProductName: strings.Join(productNames, "、"), Reason: "客户意向选择了该货运批次，请复核最终运费、开船日与到港日", CreatedBy: op.ID, CreatedByName: op.Name})
			if shippingErr != nil {
				return shippingErr
			}
			if taskErr := q.CreateFinalShipmentRecheckTask(ctx, store.CreateFinalShipmentRecheckTaskParams{TenantID: tenantID, SelectionID: selectionID, SelectionShipmentID: &selectionShipmentID, ShippingReworkID: &shippingReworkID}); taskErr != nil {
				return taskErr
			}
		}
		after, _ := json.Marshal(map[string]any{"selectionId": selectionID, "salesPlanId": in.SalesPlanID, "selectedProducts": len(candidates)})
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: in.CaseID, Section: "CUSTOMER_SELECTION", Action: "CUSTOMER_INTENT_RECORDED", EntityID: selectionID, Summary: "销售登记客户意向并发起最终复询", BeforeJson: []byte(`{}`), AfterJson: after, Reason: in.ConfirmationNote, OperatorID: op.ID, OperatorName: op.Name})
	})
	if err != nil {
		return CustomerSelectionView{}, err
	}
	s.nudge(ctx, tenantID)
	rows, err := s.ListCustomerSelections(ctx, tenantID, in.CaseID, op)
	if err != nil {
		return CustomerSelectionView{}, err
	}
	for _, row := range rows {
		if row.Header.ID == selectionID {
			return row, nil
		}
	}
	return CustomerSelectionView{}, apierr.NotFound("SC_CUSTOMER_SELECTION_NOT_FOUND", "客户最终选择不存在")
}

func (s *Service) ListCustomerSelections(ctx context.Context, tenantID, caseID int64, op Operator) ([]CustomerSelectionView, error) {
	if _, err := s.requireResponsibleSales(ctx, tenantID, caseID, op); err != nil {
		return nil, err
	}
	if err := s.q.InvalidateExpiredCustomerSelections(ctx, store.InvalidateExpiredCustomerSelectionsParams{TenantID: tenantID, CaseID: caseID}); err != nil {
		return nil, err
	}
	headers, err := s.q.ListCustomerSelections(ctx, store.ListCustomerSelectionsParams{TenantID: tenantID, CaseID: caseID})
	if err != nil {
		return nil, err
	}
	out := make([]CustomerSelectionView, 0, len(headers))
	for _, header := range headers {
		items, itemErr := s.q.ListCustomerSelectionItems(ctx, store.ListCustomerSelectionItemsParams{TenantID: tenantID, SelectionID: header.ID})
		if itemErr != nil {
			return nil, itemErr
		}
		shipmentRows, shipmentErr := s.q.ListCustomerSelectionShipments(ctx, store.ListCustomerSelectionShipmentsParams{TenantID: tenantID, SelectionID: header.ID})
		if shipmentErr != nil {
			return nil, shipmentErr
		}
		shipments := make([]CustomerSelectionShipmentView, 0, len(shipmentRows))
		for _, shipment := range shipmentRows {
			itemIDs, idsErr := s.q.ListCustomerSelectionShipmentItemIDs(ctx, store.ListCustomerSelectionShipmentItemIDsParams{TenantID: tenantID, SelectionShipmentID: shipment.ID})
			if idsErr != nil {
				return nil, idsErr
			}
			shipments = append(shipments, CustomerSelectionShipmentView{Header: shipment, SelectionItemIDs: itemIDs})
		}
		tasks, taskErr := s.q.ListFinalRecheckTasks(ctx, store.ListFinalRecheckTasksParams{TenantID: tenantID, SelectionID: header.ID})
		if taskErr != nil {
			return nil, taskErr
		}
		// Selections completed before migration 00048 only contain a free-text
		// resolution. Present them as historical/invalid instead of inviting
		// Sales to confirm a customer decision from incomplete final data.
		if header.Status == "AWAITING_CUSTOMER_CONFIRMATION" && !structuredFinalRechecksComplete(tasks) {
			header.Status = "INVALIDATED"
			header.InvalidatedReason = "流程升级：原复询缺少结构化最终结果，请重新登记客户意向"
		}
		out = append(out, CustomerSelectionView{Header: header, Items: items, Shipments: shipments, Tasks: tasks})
	}
	return out, nil
}

func (s *Service) DecideCustomerSelection(ctx context.Context, tenantID int64, in DecideCustomerSelectionInput, op Operator) (CustomerSelectionView, error) {
	in.CustomerContact, in.DecisionNote, in.DecidedAt = strings.TrimSpace(in.CustomerContact), strings.TrimSpace(in.DecisionNote), strings.TrimSpace(in.DecidedAt)
	if in.CaseID == 0 || in.SelectionID == 0 || in.DecisionNote == "" || in.DecidedAt == "" {
		return CustomerSelectionView{}, apierr.Invalid("SC_CUSTOMER_DECISION_REQUIRED", "请填写客户决定时间和说明")
	}
	if _, err := time.Parse(time.RFC3339, in.DecidedAt); err != nil {
		return CustomerSelectionView{}, apierr.Invalid("SC_CUSTOMER_DECISION_TIME", "客户决定时间格式无效")
	}
	if _, err := s.requireResponsibleSales(ctx, tenantID, in.CaseID, op); err != nil {
		return CustomerSelectionView{}, err
	}
	rows, err := s.ListCustomerSelections(ctx, tenantID, in.CaseID, op)
	if err != nil {
		return CustomerSelectionView{}, err
	}
	var current *CustomerSelectionView
	for i := range rows {
		if rows[i].Header.ID == in.SelectionID {
			current = &rows[i]
			break
		}
	}
	if current == nil || current.Header.Status != "AWAITING_CUSTOMER_CONFIRMATION" {
		return CustomerSelectionView{}, apierr.Conflict("SC_CUSTOMER_DECISION_STATE", "最终复询尚未全部完成或该意向已处理")
	}
	if !structuredFinalRechecksComplete(current.Tasks) {
		return CustomerSelectionView{}, apierr.Conflict("SC_CUSTOMER_RECHECK_DATA", "最终复询资料不完整，请由原采购或船运报价人重新完成结构化复询")
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if in.Accepted {
			itemIDs, shipmentIDs := map[int64]bool{}, map[int64]bool{}
			for _, item := range current.Items {
				itemIDs[item.ID] = true
			}
			for _, shipment := range current.Shipments {
				shipmentIDs[shipment.Header.ID] = true
			}
			if len(in.ItemPrices) != len(itemIDs) || len(in.ShipmentPrices) != len(shipmentIDs) {
				return apierr.Invalid("SC_CUSTOMER_FINAL_PRICE_REQUIRED", "请填写全部入选产品单价和船运批次运费")
			}
			for _, price := range in.ItemPrices {
				price.Currency = strings.ToUpper(strings.TrimSpace(price.Currency))
				price.PaymentTerms, price.Incoterm = strings.TrimSpace(price.PaymentTerms), strings.TrimSpace(price.Incoterm)
				if !itemIDs[price.SelectionItemID] || len(price.Currency) != 3 || !positiveDecimal(price.UnitPrice) || price.PaymentTerms == "" || price.Incoterm == "" || !validDate(price.RequiredDate) {
					return apierr.Invalid("SC_CUSTOMER_FINAL_ITEM_TERMS", "请完整填写最终对客产品价格、付款条件、贸易条款和客户要求日期")
				}
				if err := q.SetFinalCustomerItemPrice(ctx, store.SetFinalCustomerItemPriceParams{TenantID: tenantID, SelectionID: in.SelectionID, FinalCustomerCurrency: &price.Currency, FinalCustomerUnitPrice: price.UnitPrice, FinalCustomerPaymentTerms: price.PaymentTerms, FinalCustomerIncoterm: price.Incoterm, FinalCustomerRequiredDate: price.RequiredDate, ID: price.SelectionItemID}); err != nil {
					return err
				}
			}
			for _, price := range in.ShipmentPrices {
				price.Currency = strings.ToUpper(strings.TrimSpace(price.Currency))
				if !shipmentIDs[price.SelectionShipmentID] || len(price.Currency) != 3 || !positiveDecimal(price.FreightAmount) {
					return apierr.Invalid("SC_CUSTOMER_FINAL_SHIPPING_PRICE", "最终对客运费无效")
				}
				if err := q.SetFinalCustomerShipmentPrice(ctx, store.SetFinalCustomerShipmentPriceParams{TenantID: tenantID, SelectionID: in.SelectionID, FinalCustomerCurrency: &price.Currency, FinalCustomerFreightAmount: price.FreightAmount, ID: price.SelectionShipmentID}); err != nil {
					return err
				}
			}
			count, updateErr := q.AcceptCustomerSelection(ctx, store.AcceptCustomerSelectionParams{CustomerContact: in.CustomerContact, CustomerDecidedAt: in.DecidedAt, DecisionNote: in.DecisionNote, TenantID: tenantID, ID: in.SelectionID})
			if updateErr != nil {
				return updateErr
			}
			if count != 1 {
				return apierr.Conflict("SC_CUSTOMER_DECISION_STATE", "客户意向状态已经变化")
			}
		} else {
			count, updateErr := q.RejectCustomerSelection(ctx, store.RejectCustomerSelectionParams{CustomerContact: in.CustomerContact, CustomerDecidedAt: in.DecidedAt, DecisionNote: in.DecisionNote, TenantID: tenantID, ID: in.SelectionID})
			if updateErr != nil {
				return updateErr
			}
			if count != 1 {
				return apierr.Conflict("SC_CUSTOMER_DECISION_STATE", "客户意向状态已经变化")
			}
		}
		action, summary := "CUSTOMER_FINAL_REJECTED", "销售登记客户不接受最终复询结果"
		if in.Accepted {
			action, summary = "CUSTOMER_FINAL_CONFIRMED", "销售登记客户接受最终复询结果"
		}
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: in.CaseID, Section: "CUSTOMER_SELECTION", Action: action, EntityID: in.SelectionID, Summary: summary, BeforeJson: []byte(`{}`), AfterJson: []byte(`{}`), Reason: in.DecisionNote, OperatorID: op.ID, OperatorName: op.Name})
	})
	if err != nil {
		return CustomerSelectionView{}, err
	}
	s.nudge(ctx, tenantID)
	rows, err = s.ListCustomerSelections(ctx, tenantID, in.CaseID, op)
	if err != nil {
		return CustomerSelectionView{}, err
	}
	for _, row := range rows {
		if row.Header.ID == in.SelectionID {
			return row, nil
		}
	}
	return CustomerSelectionView{}, apierr.NotFound("SC_CUSTOMER_SELECTION_NOT_FOUND", "客户意向不存在")
}

func structuredFinalRechecksComplete(tasks []store.ListFinalRecheckTasksRow) bool {
	if len(tasks) == 0 {
		return false
	}
	for _, task := range tasks {
		valid := task.Status == "RESOLVED" && task.ResultNote != "" && task.ResolvedBy != 0 && len(task.FinalCurrency) == 3 && task.FinalValidUntil != ""
		if task.TaskDomain == "PROCUREMENT" {
			valid = valid && positiveDecimal(task.FinalUnitPrice) && positiveDecimal(task.FinalAvailableQty) && task.FinalLeadTime > 0 && task.FinalDeliveryDate != "" && task.FinalPaymentTerms != "" && task.FinalIncoterm != ""
		} else if task.TaskDomain == "SHIPPING" {
			valid = valid && positiveDecimal(task.FinalFreightAmount) && task.FinalEstimatedDeparture != "" && task.FinalEstimatedArrival != ""
		} else {
			valid = false
		}
		if !valid {
			return false
		}
	}
	return true
}
