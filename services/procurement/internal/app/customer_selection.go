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
	CustomerContact, ConfirmationNote string
	CustomerConfirmedAt               string
	ShipmentChoices                   []CustomerShipmentChoiceInput
}

type CustomerShipmentChoiceInput struct {
	ShipmentGroupKey      string
	SalesShippingOptionID int64
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
	if in.CaseID == 0 || in.SalesPlanID == 0 || len(in.SalesPlanItemIDs) == 0 || in.CustomerConfirmedAt == "" {
		return CustomerSelectionView{}, apierr.Invalid("SC_CUSTOMER_SELECTION_REQUIRED", "请选择客户最终接受的产品方案并填写确认时间")
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
		candidates := make([]store.CustomerSelectionCandidateRow, 0, len(in.SalesPlanItemIDs))
		batchLines := map[string]map[int64]bool{}
		selectedByLine := map[int64]store.CustomerSelectionCandidateRow{}
		for _, itemID := range in.SalesPlanItemIDs {
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
		shipmentChoiceLines := map[string][]store.CustomerShipmentCandidateLinesRow{}
		seenShippingOption := map[int64]bool{}
		for _, choice := range in.ShipmentChoices {
			choice.ShipmentGroupKey = strings.TrimSpace(choice.ShipmentGroupKey)
			requiredLines := batchLines[choice.ShipmentGroupKey]
			if choice.ShipmentGroupKey == "" || choice.SalesShippingOptionID == 0 || len(requiredLines) == 0 {
				return apierr.Invalid("SC_CUSTOMER_SHIPMENT_GROUP", "船运选择不属于当前已选供应商批次")
			}
			if _, duplicate := shipmentChoices[choice.ShipmentGroupKey]; duplicate {
				return apierr.Invalid("SC_CUSTOMER_SHIPMENT_DUPLICATE", "同一货运批次只能选择一个船运方案")
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
					needed, neededErr := decimal.NewFromString(selected.SpiQuotedQty)
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
		if invalidateErr := q.InvalidateActiveCustomerSelections(ctx, store.InvalidateActiveCustomerSelectionsParams{Reason: "客户提交了新的最终选择", TenantID: tenantID, CaseID: in.CaseID}); invalidateErr != nil {
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
			selectionItemID, itemErr := q.CreateCustomerSelectionItem(ctx, store.CreateCustomerSelectionItemParams{TenantID: tenantID, SelectionID: selectionID, SalesPlanItemID: candidate.SalesPlanItemID, SourcingLineID: candidate.SourcingLineID, ProcurementPlanItemID: candidate.ProcurementPlanItemID, SupplierQuoteLineID: candidate.SupplierQuoteLineID, ProductName: candidate.ProductName, ConfirmedQty: candidate.SpiQuotedQty, UomCode: candidate.UomCode, CustomerCurrency: candidate.CustomerCurrency, CustomerUnitPrice: candidate.SpiCustomerUnitPrice, PromisedDeliveryDate: candidate.PromisedDeliveryDate, LineNote: candidate.LineNote, SupplierID: candidate.SupplierID, SupplierName: candidate.SupplierName, FactoryID: candidate.FactoryID, FactoryName: candidate.FactoryName, ShipmentGroupKey: groupKey})
			if itemErr != nil {
				return itemErr
			}
			selectionItemIDsByGroup[groupKey] = append(selectionItemIDsByGroup[groupKey], selectionItemID)
			reason := "客户已确认该方案，请复核最终价格、可供数量与交期"
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
			shippingReworkID, shippingErr := q.CreateShippingRework(ctx, store.CreateShippingReworkParams{TenantID: tenantID, CaseID: in.CaseID, SalesPlanID: in.SalesPlanID, SourcingLineID: firstLine.SourcingLineID, ShippingOptionLineID: firstLine.ShippingOptionLineID, RequestType: "REQUOTE", ScopeType: "QUOTE", AssignedShippingID: shipping.ShippingEmployeeID, AssignedShippingName: shipping.ShippingEmployeeName, CarrierForwarder: shipping.CarrierForwarder, ProductName: strings.Join(productNames, "、"), Reason: "客户已确认该货运批次，请复核最终运费、开船日与到港日", CreatedBy: op.ID, CreatedByName: op.Name})
			if shippingErr != nil {
				return shippingErr
			}
			if taskErr := q.CreateFinalShipmentRecheckTask(ctx, store.CreateFinalShipmentRecheckTaskParams{TenantID: tenantID, SelectionID: selectionID, SelectionShipmentID: &selectionShipmentID, ShippingReworkID: &shippingReworkID}); taskErr != nil {
				return taskErr
			}
		}
		after, _ := json.Marshal(map[string]any{"selectionId": selectionID, "salesPlanId": in.SalesPlanID, "selectedProducts": len(candidates)})
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: in.CaseID, Section: "CUSTOMER_SELECTION", Action: "FINAL_SELECTION_CONFIRMED", EntityID: selectionID, Summary: "销售登记客户最终选择并发起最终复询", BeforeJson: []byte(`{}`), AfterJson: after, Reason: in.ConfirmationNote, OperatorID: op.ID, OperatorName: op.Name})
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
		out = append(out, CustomerSelectionView{Header: header, Items: items, Shipments: shipments, Tasks: tasks})
	}
	return out, nil
}
