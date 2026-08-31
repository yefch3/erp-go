package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

type ConfirmCustomerSelectionInput struct {
	CaseID, SalesPlanID               int64
	SalesPlanItemIDs                  []int64
	CustomerContact, ConfirmationNote string
	CustomerConfirmedAt               string
}

type CustomerSelectionView struct {
	Header store.ListCustomerSelectionsRow
	Items  []store.ListCustomerSelectionItemsRow
	Tasks  []store.ListFinalRecheckTasksRow
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
		}
		if invalidateErr := q.InvalidateActiveCustomerSelections(ctx, store.InvalidateActiveCustomerSelectionsParams{Reason: "客户提交了新的最终选择", TenantID: tenantID, CaseID: in.CaseID}); invalidateErr != nil {
			return invalidateErr
		}
		id, createErr := q.CreateCustomerSelection(ctx, store.CreateCustomerSelectionParams{TenantID: tenantID, CaseID: in.CaseID, SalesPlanID: in.SalesPlanID, RequirementVersionNo: caseRow.RequirementVersionNo, CustomerContact: in.CustomerContact, ConfirmationNote: in.ConfirmationNote, CustomerConfirmedAt: in.CustomerConfirmedAt, CreatedBy: op.ID, CreatedByName: op.Name})
		if createErr != nil {
			return createErr
		}
		selectionID = id
		for _, candidate := range candidates {
			shippingPlanItemID := int64(0)
			if candidate.ShippingPlanItemID != nil {
				shippingPlanItemID = *candidate.ShippingPlanItemID
			}
			selectionItemID, itemErr := q.CreateCustomerSelectionItem(ctx, store.CreateCustomerSelectionItemParams{TenantID: tenantID, SelectionID: selectionID, SalesPlanItemID: candidate.SalesPlanItemID, SourcingLineID: candidate.SourcingLineID, ProcurementPlanItemID: candidate.ProcurementPlanItemID, SupplierQuoteLineID: candidate.SupplierQuoteLineID, ShippingPlanItemID: shippingPlanItemID, ShippingOptionLineID: candidate.ShippingOptionLineID, ProductName: candidate.ProductName, ConfirmedQty: candidate.SpiQuotedQty, UomCode: candidate.UomCode, CustomerCurrency: candidate.CustomerCurrency, CustomerUnitPrice: candidate.SpiCustomerUnitPrice, PromisedDeliveryDate: candidate.PromisedDeliveryDate, LineNote: candidate.LineNote})
			if itemErr != nil {
				return itemErr
			}
			reason := "客户已确认该方案，请复核最终价格、可供数量与交期"
			procurementReworkID, reworkErr := q.CreateProcurementReworkRequest(ctx, store.CreateProcurementReworkRequestParams{TenantID: tenantID, CaseID: in.CaseID, PlanID: plan.ProcurementPlanID, SourcingLineID: candidate.SourcingLineID, SupplierQuoteLineID: candidate.SupplierQuoteLineID, RequestType: "REQUOTE", ScopeType: "QUOTE", AssignedBuyerID: candidate.BuyerID, AssignedBuyerName: candidate.BuyerName, SupplierID: candidate.SupplierID, SupplierName: candidate.SupplierName, ProductName: candidate.ProductName, Reason: reason, CreatedBy: op.ID, CreatedByName: op.Name})
			if reworkErr != nil {
				return reworkErr
			}
			if taskErr := q.CreateFinalProcurementRecheckTask(ctx, store.CreateFinalProcurementRecheckTaskParams{TenantID: tenantID, SelectionID: selectionID, SelectionItemID: selectionItemID, ProcurementReworkID: &procurementReworkID}); taskErr != nil {
				return taskErr
			}
			if candidate.ShippingOptionLineID != 0 {
				shippingReworkID, shippingErr := q.CreateShippingRework(ctx, store.CreateShippingReworkParams{TenantID: tenantID, CaseID: in.CaseID, SalesPlanID: in.SalesPlanID, SourcingLineID: candidate.SourcingLineID, ShippingOptionLineID: candidate.ShippingOptionLineID, RequestType: "REQUOTE", ScopeType: "QUOTE", AssignedShippingID: candidate.ShippingEmployeeID, AssignedShippingName: candidate.ShippingEmployeeName, CarrierForwarder: candidate.CarrierForwarder, ProductName: candidate.ProductName, Reason: "客户已确认该船运方案，请复核最终运费、开船日与到港日", CreatedBy: op.ID, CreatedByName: op.Name})
				if shippingErr != nil {
					return shippingErr
				}
				if taskErr := q.CreateFinalShippingRecheckTask(ctx, store.CreateFinalShippingRecheckTaskParams{TenantID: tenantID, SelectionID: selectionID, SelectionItemID: selectionItemID, ShippingReworkID: &shippingReworkID}); taskErr != nil {
					return taskErr
				}
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
		tasks, taskErr := s.q.ListFinalRecheckTasks(ctx, store.ListFinalRecheckTasksParams{TenantID: tenantID, SelectionID: header.ID})
		if taskErr != nil {
			return nil, taskErr
		}
		out = append(out, CustomerSelectionView{Header: header, Items: items, Tasks: tasks})
	}
	return out, nil
}
