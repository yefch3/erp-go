package app

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

type ProcurementPlanSelectionInput struct {
	SourcingLineID, SupplierQuoteLineID int64
	SelectionType                       string
	Priority                            int32
	Reason, Risk                        string
}

type NewProcurementPlan struct {
	CaseID      int64
	ManagerNote string
	Selections  []ProcurementPlanSelectionInput
}

type ProcurementPlanHeader struct {
	ID, CaseID, TargetSalesID                                   int64
	PlanNo, Status, ManagerNote, CreatedByName, ConfirmedByName string
	SubmittedToSalesByName, TargetSalesName                     string
	VersionNo, RequirementVersionNo                             int32
	ConfirmedAt, SubmittedToSalesAt, CreatedAt                  string
}

type ProcurementPlanView struct {
	Header ProcurementPlanHeader
	Items  []store.ListProcurementPlanItemsRow
}

type NewProcurementRework struct {
	CaseID, PlanID, SourcingLineID, SupplierQuoteLineID int64
	RequestType, Reason                                 string
}

func planHeaderFromGet(row store.GetProcurementPlanRow) ProcurementPlanHeader {
	return ProcurementPlanHeader{
		ID: row.ID, CaseID: row.CaseID, PlanNo: row.PlanNo, VersionNo: row.VersionNo,
		RequirementVersionNo: row.RequirementVersionNo, Status: row.Status, ManagerNote: row.ManagerNote,
		CreatedByName: row.CreatedByName, ConfirmedByName: row.ConfirmedByName,
		ConfirmedAt: planTimeText(row.ConfirmedAt), SubmittedToSalesByName: row.SubmittedToSalesByName,
		SubmittedToSalesAt: planTimeText(row.SubmittedToSalesAt), TargetSalesID: row.TargetSalesID,
		TargetSalesName: row.TargetSalesName, CreatedAt: planTimeText(row.CreatedAt),
	}
}

func planHeaderFromList(row store.ListProcurementPlansRow) ProcurementPlanHeader {
	return ProcurementPlanHeader{
		ID: row.ID, CaseID: row.CaseID, PlanNo: row.PlanNo, VersionNo: row.VersionNo,
		RequirementVersionNo: row.RequirementVersionNo, Status: row.Status, ManagerNote: row.ManagerNote,
		CreatedByName: row.CreatedByName, ConfirmedByName: row.ConfirmedByName,
		ConfirmedAt: planTimeText(row.ConfirmedAt), SubmittedToSalesByName: row.SubmittedToSalesByName,
		SubmittedToSalesAt: planTimeText(row.SubmittedToSalesAt), TargetSalesID: row.TargetSalesID,
		TargetSalesName: row.TargetSalesName, CreatedAt: planTimeText(row.CreatedAt),
	}
}

func planTimeText(v pgtype.Timestamptz) string {
	if !v.Valid {
		return ""
	}
	return v.Time.UTC().Format(time.RFC3339)
}

// CreateProcurementPlan 将采购经理选中的报价快照整合为一个不可覆盖的统一方案版本。
// 方案不分配采购数量；推荐与备选都引用采购已核实的原始报价，经理不能修改报价内容。
func (s *Service) CreateProcurementPlan(ctx context.Context, tenantID int64, in NewProcurementPlan, op Operator) (ProcurementPlanView, error) {
	in.ManagerNote = strings.TrimSpace(in.ManagerNote)
	if in.CaseID == 0 || len(in.Selections) == 0 || in.ManagerNote == "" {
		return ProcurementPlanView{}, apierr.Invalid("SC_PLAN_REQUIRED", "请选择报价并填写经理意见")
	}
	caseRow, err := s.q.ProcurementPlanCase(ctx, store.ProcurementPlanCaseParams{TenantID: tenantID, ID: in.CaseID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ProcurementPlanView{}, apierr.NotFound("SC_CASE_NOT_FOUND", "询价案件不存在")
	}
	if err != nil {
		return ProcurementPlanView{}, err
	}
	if caseRow.OwnerID == 0 {
		return ProcurementPlanView{}, apierr.Conflict("SC_PLAN_SALES_REQUIRED", "案件没有负责销售，不能生成定向方案")
	}
	if caseRow.Status != "QUOTES_RECEIVED" && caseRow.Status != "COSTING" {
		return ProcurementPlanView{}, apierr.Conflict("SC_PLAN_QUOTES_REQUIRED", "当前案件不在报价比较阶段")
	}
	if caseRow.HandoffStatus != "IN_PROGRESS" && caseRow.HandoffStatus != "PROCUREMENT_PLAN_READY" && caseRow.HandoffStatus != "PROCUREMENT_PLAN_SUBMITTED" {
		return ProcurementPlanView{}, apierr.Conflict("SC_PLAN_HANDOFF_INVALID", "当前案件不能重新生成采购统一方案")
	}
	expected, err := s.q.CountProcurementPlanLines(ctx, store.CountProcurementPlanLinesParams{TenantID: tenantID, CaseID: in.CaseID})
	if err != nil {
		return ProcurementPlanView{}, err
	}
	candidates := make([]store.ProcurementPlanCandidateRow, 0, len(in.Selections))
	recommendedLines, selectedQuotes, priorities := map[int64]bool{}, map[int64]bool{}, map[string]bool{}
	for i := range in.Selections {
		selection := &in.Selections[i]
		selection.SelectionType = strings.ToUpper(strings.TrimSpace(selection.SelectionType))
		selection.Reason, selection.Risk = strings.TrimSpace(selection.Reason), strings.TrimSpace(selection.Risk)
		if selection.SelectionType != "RECOMMENDED" && selection.SelectionType != "BACKUP" {
			return ProcurementPlanView{}, apierr.Invalid("SC_PLAN_TYPE_INVALID", "方案只能标记为推荐或备选")
		}
		if selection.Priority <= 0 {
			return ProcurementPlanView{}, apierr.Invalid("SC_PLAN_PRIORITY_INVALID", "请填写大于 0 的推荐顺序")
		}
		if selection.SelectionType == "RECOMMENDED" && selection.Reason == "" {
			return ProcurementPlanView{}, apierr.Invalid("SC_PLAN_REASON_REQUIRED", "推荐报价必须填写推荐原因")
		}
		if selectedQuotes[selection.SupplierQuoteLineID] {
			return ProcurementPlanView{}, apierr.Invalid("SC_PLAN_DUPLICATE_QUOTE", "同一报价不能在统一方案中重复选择")
		}
		selectedQuotes[selection.SupplierQuoteLineID] = true
		candidate, candidateErr := s.q.ProcurementPlanCandidate(ctx, store.ProcurementPlanCandidateParams{
			TenantID: tenantID, CaseID: in.CaseID, ID: selection.SupplierQuoteLineID,
		})
		if candidateErr != nil || candidate.SourcingLineID != selection.SourcingLineID {
			return ProcurementPlanView{}, apierr.Invalid("SC_PLAN_QUOTE_INVALID", "所选报价已过期、不是最新版本或不属于该产品")
		}
		priorityKey := strconv.FormatInt(selection.SourcingLineID, 10) + ":" + selection.SelectionType + ":" + strconv.Itoa(int(selection.Priority))
		if priorities[priorityKey] {
			return ProcurementPlanView{}, apierr.Invalid("SC_PLAN_PRIORITY_DUPLICATE", "同一产品的推荐或备选顺序不能重复")
		}
		priorities[priorityKey] = true
		if selection.SelectionType == "RECOMMENDED" {
			recommendedLines[selection.SourcingLineID] = true
		}
		candidates = append(candidates, candidate)
	}
	if int64(len(recommendedLines)) != expected {
		return ProcurementPlanView{}, apierr.Invalid("SC_PLAN_INCOMPLETE", "每个有效产品至少需要选择一个推荐报价")
	}
	var planID int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, lockErr := q.LockCostScenarioCase(ctx, store.LockCostScenarioCaseParams{TenantID: tenantID, ID: in.CaseID}); lockErr != nil {
			return lockErr
		}
		if err := q.SupersedeConfirmedProcurementPlans(ctx, store.SupersedeConfirmedProcurementPlansParams{TenantID: tenantID, CaseID: in.CaseID}); err != nil {
			return err
		}
		created, createErr := q.CreateProcurementPlan(ctx, store.CreateProcurementPlanParams{
			TenantID: tenantID, CaseID: in.CaseID, RequirementVersionNo: caseRow.RequirementVersionNo,
			ManagerNote: in.ManagerNote, CreatedBy: op.ID, CreatedByName: op.Name,
			TargetSalesID: caseRow.OwnerID, TargetSalesName: caseRow.OwnerName,
		})
		if createErr != nil {
			return createErr
		}
		planID = created.ID
		for i, candidate := range candidates {
			selection := in.Selections[i]
			var leadTime *int32
			if parsed, parseErr := strconv.Atoi(strings.TrimSpace(candidate.LeadTime)); parseErr == nil && parsed > 0 {
				value := int32(parsed)
				leadTime = &value
			}
			if itemErr := q.CreateProcurementPlanItem(ctx, store.CreateProcurementPlanItemParams{
				TenantID: tenantID, PlanID: planID, SourcingLineID: candidate.SourcingLineID,
				SupplierQuoteLineID: candidate.QuoteLineID, SelectionType: selection.SelectionType,
				Priority: selection.Priority, Reason: selection.Reason, Risk: selection.Risk,
				SupplierID: candidate.SupplierID, SupplierName: candidate.SupplierName,
				FactoryID: candidate.FactoryID, FactoryName: candidate.FactoryName,
				BuyerID: candidate.BuyerID, BuyerName: candidate.BuyerName, ProductName: candidate.ProductName,
				Currency: candidate.Currency, UnitPrice: candidate.QlUnitPrice, AvailableQty: candidate.QlQty,
				UomCode: candidate.UomCode, Moq: candidate.Moq, LeadTime: leadTime,
				PaymentTerms: candidate.PaymentTerms, Incoterm: candidate.Incoterm,
				ValidUntil: candidate.ValidUntil, QuoteVersionNo: candidate.QuoteVersionNo,
			}); itemErr != nil {
				return itemErr
			}
		}
		if markErr := q.MarkSourcingCaseProcurementPlanReady(ctx, store.MarkSourcingCaseProcurementPlanReadyParams{TenantID: tenantID, ID: in.CaseID}); markErr != nil {
			return markErr
		}
		after, _ := json.Marshal(map[string]any{"planNo": created.PlanNo, "version": created.VersionNo, "selectedQuotes": len(in.Selections), "targetSales": caseRow.OwnerName})
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{
			TenantID: tenantID, CaseID: in.CaseID, Section: "PROCUREMENT_PLAN", Action: "CONFIRMED",
			EntityID: planID, Summary: "采购经理确认统一采购方案 " + created.PlanNo,
			BeforeJson: []byte(`{}`), AfterJson: after, OperatorID: op.ID, OperatorName: op.Name,
		})
	})
	if err != nil {
		return ProcurementPlanView{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetProcurementPlan(ctx, tenantID, planID)
}

func (s *Service) GetProcurementPlan(ctx context.Context, tenantID, id int64) (ProcurementPlanView, error) {
	header, err := s.q.GetProcurementPlan(ctx, store.GetProcurementPlanParams{TenantID: tenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return ProcurementPlanView{}, apierr.NotFound("SC_PLAN_NOT_FOUND", "统一采购方案不存在")
	}
	if err != nil {
		return ProcurementPlanView{}, err
	}
	items, err := s.q.ListProcurementPlanItems(ctx, store.ListProcurementPlanItemsParams{TenantID: tenantID, PlanID: id})
	return ProcurementPlanView{Header: planHeaderFromGet(header), Items: items}, err
}

func (s *Service) ListProcurementPlans(ctx context.Context, tenantID, caseID int64) ([]ProcurementPlanView, error) {
	if _, err := s.q.ProcurementPlanCase(ctx, store.ProcurementPlanCaseParams{TenantID: tenantID, ID: caseID}); err != nil {
		return nil, apierr.NotFound("SC_CASE_NOT_FOUND", "询价案件不存在")
	}
	headers, err := s.q.ListProcurementPlans(ctx, store.ListProcurementPlansParams{TenantID: tenantID, CaseID: caseID})
	if err != nil {
		return nil, err
	}
	result := make([]ProcurementPlanView, 0, len(headers))
	for _, header := range headers {
		items, itemErr := s.q.ListProcurementPlanItems(ctx, store.ListProcurementPlanItemsParams{TenantID: tenantID, PlanID: header.ID})
		if itemErr != nil {
			return nil, itemErr
		}
		result = append(result, ProcurementPlanView{Header: planHeaderFromList(header), Items: items})
	}
	return result, nil
}

func (s *Service) SubmitProcurementPlanToSales(ctx context.Context, tenantID, id int64, op Operator) (ProcurementPlanView, error) {
	current, err := s.GetProcurementPlan(ctx, tenantID, id)
	if err != nil {
		return ProcurementPlanView{}, err
	}
	if current.Header.Status != "CONFIRMED" {
		return ProcurementPlanView{}, apierr.Conflict("SC_PLAN_NOT_SUBMITTABLE", "只有已确认且未提交的统一采购方案可以提交销售")
	}
	caseRow, err := s.q.ProcurementPlanCase(ctx, store.ProcurementPlanCaseParams{TenantID: tenantID, ID: current.Header.CaseID})
	if err != nil {
		return ProcurementPlanView{}, err
	}
	if caseRow.OwnerID != current.Header.TargetSalesID {
		return ProcurementPlanView{}, apierr.Conflict("SC_PLAN_SALES_CHANGED", "案件负责销售已变化，请生成新的统一采购方案")
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		count, submitErr := q.SubmitProcurementPlanToSales(ctx, store.SubmitProcurementPlanToSalesParams{
			OperatorID: &op.ID, OperatorName: op.Name, TenantID: tenantID, ID: id,
		})
		if submitErr != nil {
			return submitErr
		}
		if count != 1 {
			return apierr.Conflict("SC_PLAN_ALREADY_SUBMITTED", "统一采购方案已经提交销售")
		}
		if markErr := q.MarkSourcingCaseProcurementPlanSubmitted(ctx, store.MarkSourcingCaseProcurementPlanSubmittedParams{TenantID: tenantID, ID: current.Header.CaseID}); markErr != nil {
			return markErr
		}
		after, _ := json.Marshal(map[string]any{"planNo": current.Header.PlanNo, "targetSalesId": caseRow.OwnerID, "targetSalesName": caseRow.OwnerName})
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{
			TenantID: tenantID, CaseID: current.Header.CaseID, Section: "HANDOFF", Action: "PROCUREMENT_PLAN_SUBMITTED",
			EntityID: id, Summary: "统一采购方案已定向提交负责销售 " + caseRow.OwnerName,
			BeforeJson: []byte(`{"handoffStatus":"PROCUREMENT_PLAN_READY"}`), AfterJson: after,
			OperatorID: op.ID, OperatorName: op.Name,
		})
	})
	if err != nil {
		return ProcurementPlanView{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetProcurementPlan(ctx, tenantID, id)
}

func (s *Service) CreateProcurementRework(ctx context.Context, tenantID int64, in NewProcurementRework, op Operator) (store.ListProcurementReworkRequestsRow, error) {
	in.RequestType, in.Reason = strings.ToUpper(strings.TrimSpace(in.RequestType)), strings.TrimSpace(in.Reason)
	if in.CaseID == 0 || in.Reason == "" {
		return store.ListProcurementReworkRequestsRow{}, apierr.Invalid("SC_REWORK_REQUIRED", "请选择退回范围并填写原因")
	}
	if in.RequestType != "REQUOTE" && in.RequestType != "RENEGOTIATE" && in.RequestType != "ADD_SUPPLIER" {
		return store.ListProcurementReworkRequestsRow{}, apierr.Invalid("SC_REWORK_TYPE_INVALID", "退回要求无效")
	}
	params := store.CreateProcurementReworkRequestParams{
		TenantID: tenantID, CaseID: in.CaseID, PlanID: in.PlanID, RequestType: in.RequestType,
		Reason: in.Reason, CreatedBy: op.ID, CreatedByName: op.Name,
	}
	if in.SupplierQuoteLineID != 0 {
		quote, err := s.q.ProcurementReworkQuote(ctx, store.ProcurementReworkQuoteParams{TenantID: tenantID, CaseID: in.CaseID, ID: in.SupplierQuoteLineID})
		if err != nil {
			return store.ListProcurementReworkRequestsRow{}, apierr.Invalid("SC_REWORK_QUOTE_INVALID", "要退回的报价不存在")
		}
		params.SourcingLineID, params.SupplierQuoteLineID, params.ScopeType = quote.SourcingLineID, quote.QuoteLineID, "QUOTE"
		params.AssignedBuyerID, params.AssignedBuyerName = quote.BuyerID, quote.BuyerName
		params.SupplierID, params.SupplierName, params.ProductName = quote.SupplierID, quote.SupplierName, quote.ProductName
	} else if in.SourcingLineID != 0 {
		product, err := s.q.ProcurementReworkProduct(ctx, store.ProcurementReworkProductParams{TenantID: tenantID, CaseID: in.CaseID, ID: in.SourcingLineID})
		if err != nil {
			return store.ListProcurementReworkRequestsRow{}, apierr.Invalid("SC_REWORK_PRODUCT_INVALID", "要补充供应商的产品不存在")
		}
		params.SourcingLineID, params.ScopeType, params.ProductName = product.ID, "PRODUCT", product.Product
	} else {
		params.ScopeType = "ALL"
	}
	if params.ScopeType != "QUOTE" && in.RequestType != "ADD_SUPPLIER" {
		return store.ListProcurementReworkRequestsRow{}, apierr.Invalid("SC_REWORK_TARGET_REQUIRED", "重新询价或议价必须指定现有报价")
	}
	var id int64
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		createdID, createErr := q.CreateProcurementReworkRequest(ctx, params)
		if createErr != nil {
			return createErr
		}
		id = createdID
		after, _ := json.Marshal(map[string]any{"requestId": id, "type": in.RequestType, "scope": params.ScopeType, "buyer": params.AssignedBuyerName, "supplier": params.SupplierName, "product": params.ProductName})
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{
			TenantID: tenantID, CaseID: in.CaseID, Section: "PROCUREMENT_REWORK", Action: "CREATED", EntityID: id,
			Summary: "采购经理要求补充或重新处理报价", BeforeJson: []byte(`{}`), AfterJson: after,
			Reason: in.Reason, OperatorID: op.ID, OperatorName: op.Name,
		})
	})
	if err != nil {
		return store.ListProcurementReworkRequestsRow{}, err
	}
	s.nudge(ctx, tenantID)
	rows, err := s.ListProcurementReworks(ctx, tenantID, in.CaseID)
	if err != nil {
		return store.ListProcurementReworkRequestsRow{}, err
	}
	for _, row := range rows {
		if row.ID == id {
			return row, nil
		}
	}
	return store.ListProcurementReworkRequestsRow{}, apierr.NotFound("SC_REWORK_NOT_FOUND", "退回任务不存在")
}

func (s *Service) ListProcurementReworks(ctx context.Context, tenantID, caseID int64) ([]store.ListProcurementReworkRequestsRow, error) {
	return s.q.ListProcurementReworkRequests(ctx, store.ListProcurementReworkRequestsParams{TenantID: tenantID, CaseID: caseID})
}

func (s *Service) ResolveProcurementRework(ctx context.Context, tenantID, id int64, note string, op Operator) error {
	note = strings.TrimSpace(note)
	if note == "" {
		return apierr.Invalid("SC_REWORK_NOTE_REQUIRED", "请填写本次处理结果")
	}
	request, err := s.q.GetProcurementReworkRequest(ctx, store.GetProcurementReworkRequestParams{TenantID: tenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFound("SC_REWORK_NOT_FOUND", "退回任务不存在")
	}
	if err != nil {
		return err
	}
	if request.AssignedBuyerID != 0 && request.AssignedBuyerID != op.ID {
		return apierr.Permission("SC_REWORK_ASSIGNEE_REQUIRED", "只有被指定的采购专员可以完成该任务")
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		count, resolveErr := q.ResolveProcurementReworkRequest(ctx, store.ResolveProcurementReworkRequestParams{
			OperatorID: &op.ID, OperatorName: op.Name, ResolutionNote: note, TenantID: tenantID, ID: id,
		})
		if resolveErr != nil {
			return resolveErr
		}
		if count != 1 {
			return apierr.Conflict("SC_REWORK_ALREADY_RESOLVED", "退回任务已经处理")
		}
		if resolveErr = q.ResolveFinalTaskByProcurementRework(ctx, store.ResolveFinalTaskByProcurementReworkParams{TenantID: tenantID, ProcurementReworkID: &id}); resolveErr != nil {
			return resolveErr
		}
		if resolveErr = q.CompleteReadyCustomerSelections(ctx, tenantID); resolveErr != nil {
			return resolveErr
		}
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{
			TenantID: tenantID, CaseID: request.CaseID, Section: "PROCUREMENT_REWORK", Action: "RESOLVED", EntityID: id,
			Summary: "采购专员完成报价补充任务", BeforeJson: []byte(`{"status":"OPEN"}`),
			AfterJson: []byte(`{"status":"RESOLVED"}`), Reason: note, OperatorID: op.ID, OperatorName: op.Name,
		})
	})
	if err == nil {
		s.nudge(ctx, tenantID)
	}
	return err
}
