package app

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

type ShippingPlanSelectionInput struct {
	SourcingLineID, ShippingOptionLineID int64
	SelectionType                        string
	Priority                             int32
	Reason, Risk                         string
}

type NewShippingPlan struct {
	CaseID      int64
	ManagerNote string
	Selections  []ShippingPlanSelectionInput
}

type ShippingPlanView struct {
	Header store.ListSourcingShippingPlansRow
	Items  []store.ListSourcingShippingPlanItemsRow
}

func (s *Service) ListSourcingShippingParticipants(ctx context.Context, tenantID, caseID int64) ([]store.ListSourcingShippingParticipantsRow, error) {
	request, err := s.q.GetSourcingShippingRequest(ctx, store.GetSourcingShippingRequestParams{TenantID: tenantID, CaseID: caseID})
	if err != nil {
		return nil, err
	}
	return s.q.ListSourcingShippingParticipants(ctx, store.ListSourcingShippingParticipantsParams{TenantID: tenantID, RequestID: request.ID})
}

func (s *Service) JoinSourcingShippingTask(ctx context.Context, tenantID, caseID int64, op Operator) ([]store.ListSourcingShippingParticipantsRow, error) {
	request, err := s.q.GetSourcingShippingRequest(ctx, store.GetSourcingShippingRequestParams{TenantID: tenantID, CaseID: caseID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierr.NotFound("SC_SHIPPING_NOT_FOUND", "售前船运询价案件不存在")
	}
	if err != nil {
		return nil, err
	}
	if request.Status == "CANCELLED" {
		return nil, apierr.Conflict("SC_SHIPPING_CLOSED", "该售前船运询价案件已关闭")
	}
	if err = s.q.JoinSourcingShippingRequest(ctx, store.JoinSourcingShippingRequestParams{TenantID: tenantID, RequestID: request.ID, EmployeeID: op.ID, EmployeeName: op.Name}); err != nil {
		return nil, err
	}
	_, _ = s.q.StartSourcingShippingRequest(ctx, store.StartSourcingShippingRequestParams{TenantID: tenantID, CaseID: caseID})
	_ = s.q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: caseID, Section: "SHIPPING_PARTICIPATION", Action: "PARTICIPANT_JOINED", Summary: "船运专员参与售前询价", BeforeJson: []byte(`{}`), AfterJson: []byte(`{}`), OperatorID: op.ID, OperatorName: op.Name})
	return s.ListSourcingShippingParticipants(ctx, tenantID, caseID)
}

func (s *Service) RequestPrimaryShipping(ctx context.Context, tenantID, caseID int64, op Operator) ([]store.ListSourcingShippingParticipantsRow, error) {
	participants, err := s.JoinSourcingShippingTask(ctx, tenantID, caseID, op)
	if err != nil {
		return nil, err
	}
	request, _ := s.q.GetSourcingShippingRequest(ctx, store.GetSourcingShippingRequestParams{TenantID: tenantID, CaseID: caseID})
	for _, item := range participants {
		if item.ParticipantRole == "PRIMARY" {
			_, err = s.q.RequestPrimaryShipping(ctx, store.RequestPrimaryShippingParams{TenantID: tenantID, RequestID: request.ID, EmployeeID: op.ID})
			return s.ListSourcingShippingParticipants(ctx, tenantID, caseID)
		}
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.ClearPrimaryShipping(ctx, store.ClearPrimaryShippingParams{TenantID: tenantID, RequestID: request.ID}); err != nil {
			return err
		}
		_, err := q.AssignPrimaryShipping(ctx, store.AssignPrimaryShippingParams{TenantID: tenantID, RequestID: request.ID, EmployeeID: op.ID})
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.ListSourcingShippingParticipants(ctx, tenantID, caseID)
}

func (s *Service) AssignPrimaryShipping(ctx context.Context, tenantID, caseID, employeeID int64, reason string, op Operator) ([]store.ListSourcingShippingParticipantsRow, error) {
	reason = strings.TrimSpace(reason)
	request, err := s.q.GetSourcingShippingRequest(ctx, store.GetSourcingShippingRequestParams{TenantID: tenantID, CaseID: caseID})
	if err != nil {
		return nil, err
	}
	target, err := s.q.ShippingParticipant(ctx, store.ShippingParticipantParams{TenantID: tenantID, RequestID: request.ID, EmployeeID: employeeID})
	if err != nil {
		return nil, apierr.Invalid("SC_SHIPPING_PRIMARY_PARTICIPANT", "只能从当前参与的船运专员中指定主责")
	}
	current, _ := s.q.ListSourcingShippingParticipants(ctx, store.ListSourcingShippingParticipantsParams{TenantID: tenantID, RequestID: request.ID})
	for _, item := range current {
		if item.ParticipantRole == "PRIMARY" && item.EmployeeID != employeeID && reason == "" {
			return nil, apierr.Invalid("SC_SHIPPING_PRIMARY_REASON", "更换主责船运专员必须填写原因")
		}
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.ClearPrimaryShipping(ctx, store.ClearPrimaryShippingParams{TenantID: tenantID, RequestID: request.ID}); err != nil {
			return err
		}
		if _, err := q.AssignPrimaryShipping(ctx, store.AssignPrimaryShippingParams{TenantID: tenantID, RequestID: request.ID, EmployeeID: employeeID}); err != nil {
			return err
		}
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: caseID, Section: "SHIPPING_PARTICIPATION", Action: "PRIMARY_ASSIGNED", Summary: "指定主责船运专员 " + target.EmployeeName, BeforeJson: []byte(`{}`), AfterJson: []byte(`{}`), Reason: reason, OperatorID: op.ID, OperatorName: op.Name})
	})
	if err != nil {
		return nil, err
	}
	return s.ListSourcingShippingParticipants(ctx, tenantID, caseID)
}

func (s *Service) requireShippingParticipant(ctx context.Context, tenantID, requestID, employeeID int64) error {
	if _, err := s.q.ShippingParticipant(ctx, store.ShippingParticipantParams{TenantID: tenantID, RequestID: requestID, EmployeeID: employeeID}); err != nil {
		return apierr.Permission("SC_SHIPPING_PARTICIPANT_REQUIRED", "请先参与该售前船运询价案件")
	}
	return nil
}

func (s *Service) CreateSourcingShippingPlan(ctx context.Context, tenantID int64, in NewShippingPlan, op Operator) (ShippingPlanView, error) {
	in.ManagerNote = strings.TrimSpace(in.ManagerNote)
	if in.CaseID == 0 || in.ManagerNote == "" || len(in.Selections) == 0 {
		return ShippingPlanView{}, apierr.Invalid("SC_SHIPPING_PLAN_REQUIRED", "请选择船运报价并填写经理意见")
	}
	request, err := s.q.GetSourcingShippingRequest(ctx, store.GetSourcingShippingRequestParams{TenantID: tenantID, CaseID: in.CaseID})
	if err != nil {
		return ShippingPlanView{}, err
	}
	cargoItems, err := s.q.ListSourcingShippingCargoItems(ctx, store.ListSourcingShippingCargoItemsParams{TenantID: tenantID, CaseID: in.CaseID})
	if err != nil {
		return ShippingPlanView{}, err
	}
	candidates := make([]store.SourcingShippingPlanCandidateRow, 0, len(in.Selections))
	recommended, priorities, selected := map[int64]bool{}, map[string]bool{}, map[int64]bool{}
	for i := range in.Selections {
		sel := &in.Selections[i]
		sel.SelectionType, sel.Reason, sel.Risk = strings.ToUpper(strings.TrimSpace(sel.SelectionType)), strings.TrimSpace(sel.Reason), strings.TrimSpace(sel.Risk)
		if sel.SelectionType != "RECOMMENDED" && sel.SelectionType != "BACKUP" || sel.Priority <= 0 {
			return ShippingPlanView{}, apierr.Invalid("SC_SHIPPING_PLAN_SELECTION", "请选择有效的推荐/备选类型和顺序")
		}
		if sel.SelectionType == "RECOMMENDED" && sel.Reason == "" {
			return ShippingPlanView{}, apierr.Invalid("SC_SHIPPING_PLAN_REASON", "推荐船运报价必须填写推荐原因")
		}
		if selected[sel.ShippingOptionLineID] {
			return ShippingPlanView{}, apierr.Invalid("SC_SHIPPING_PLAN_DUPLICATE", "同一船运报价不能重复选择")
		}
		candidate, candidateErr := s.q.SourcingShippingPlanCandidate(ctx, store.SourcingShippingPlanCandidateParams{TenantID: tenantID, RequestID: request.ID, ID: sel.ShippingOptionLineID})
		if candidateErr != nil || candidate.SourcingLineID != sel.SourcingLineID {
			return ShippingPlanView{}, apierr.Invalid("SC_SHIPPING_PLAN_QUOTE", "船运报价已失效、不是最新版本或不属于该货物")
		}
		key := strconv.FormatInt(sel.SourcingLineID, 10) + ":" + sel.SelectionType + ":" + strconv.Itoa(int(sel.Priority))
		if priorities[key] {
			return ShippingPlanView{}, apierr.Invalid("SC_SHIPPING_PLAN_PRIORITY", "同一货物的推荐或备选顺序不能重复")
		}
		priorities[key], selected[sel.ShippingOptionLineID] = true, true
		if sel.SelectionType == "RECOMMENDED" {
			recommended[sel.SourcingLineID] = true
		}
		candidates = append(candidates, candidate)
	}
	if len(recommended) != len(cargoItems) {
		return ShippingPlanView{}, apierr.Invalid("SC_SHIPPING_PLAN_INCOMPLETE", "必须为每一种货物选择至少一个推荐船运报价")
	}
	var planID int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.SupersedeConfirmedSourcingShippingPlans(ctx, store.SupersedeConfirmedSourcingShippingPlansParams{TenantID: tenantID, RequestID: request.ID}); err != nil {
			return err
		}
		created, err := q.CreateSourcingShippingPlan(ctx, store.CreateSourcingShippingPlanParams{TenantID: tenantID, RequestID: request.ID, RequirementVersionNo: request.RequirementVersionNo, ManagerNote: in.ManagerNote, CreatedBy: op.ID, CreatedByName: op.Name, TargetSalesID: request.SalesEmployeeID, TargetSalesName: request.SalesEmployeeName})
		if err != nil {
			return err
		}
		planID = created.ID
		for i, candidate := range candidates {
			sel := in.Selections[i]
			if err := q.CreateSourcingShippingPlanItem(ctx, store.CreateSourcingShippingPlanItemParams{TenantID: tenantID, PlanID: planID, SourcingLineID: candidate.SourcingLineID, ShippingOptionLineID: candidate.OptionLineID, SelectionType: sel.SelectionType, Priority: sel.Priority, Reason: sel.Reason, Risk: sel.Risk, CarrierForwarder: candidate.CarrierForwarder, ServiceOptionName: candidate.ServiceOptionName, ShippingEmployeeID: candidate.ShippingEmployeeID, ShippingEmployeeName: candidate.ShippingEmployeeName, ProductName: candidate.ProductName, Currency: candidate.Currency, ChargeBasis: candidate.ChargeBasis, UnitRate: candidate.OlUnitRate, TotalFreight: candidate.OlTotalFreight, PortOfLoading: candidate.PortOfLoading, PortOfDischarge: candidate.PortOfDischarge, EstimatedDeparture: candidate.EstimatedDeparture, EstimatedArrival: candidate.EstimatedArrival, ValidUntil: candidate.ValidUntil, QuoteVersionNo: candidate.QuoteVersionNo}); err != nil {
				return err
			}
		}
		return q.MarkSourcingShippingPlanReady(ctx, store.MarkSourcingShippingPlanReadyParams{TenantID: tenantID, ID: request.ID})
	})
	if err != nil {
		return ShippingPlanView{}, err
	}
	return s.GetSourcingShippingPlan(ctx, tenantID, planID)
}

func (s *Service) GetSourcingShippingPlan(ctx context.Context, tenantID, id int64) (ShippingPlanView, error) {
	header, err := s.q.GetSourcingShippingPlan(ctx, store.GetSourcingShippingPlanParams{TenantID: tenantID, ID: id})
	if err != nil {
		return ShippingPlanView{}, err
	}
	items, err := s.q.ListSourcingShippingPlanItems(ctx, store.ListSourcingShippingPlanItemsParams{TenantID: tenantID, PlanID: id})
	listHeader := store.ListSourcingShippingPlansRow{ID: header.ID, RequestID: header.RequestID, PlanNo: header.PlanNo, VersionNo: header.VersionNo, RequirementVersionNo: header.RequirementVersionNo, Status: header.Status, ManagerNote: header.ManagerNote, CreatedBy: header.CreatedBy, CreatedByName: header.CreatedByName, ConfirmedAt: header.ConfirmedAt, SubmittedToSalesBy: header.SubmittedToSalesBy, SubmittedToSalesByName: header.SubmittedToSalesByName, SubmittedToSalesAt: header.SubmittedToSalesAt, TargetSalesID: header.TargetSalesID, TargetSalesName: header.TargetSalesName, CreatedAt: header.CreatedAt}
	return ShippingPlanView{Header: listHeader, Items: items}, err
}

func (s *Service) ListSourcingShippingPlans(ctx context.Context, tenantID, caseID int64) ([]ShippingPlanView, error) {
	request, err := s.q.GetSourcingShippingRequest(ctx, store.GetSourcingShippingRequestParams{TenantID: tenantID, CaseID: caseID})
	if err != nil {
		return nil, err
	}
	headers, err := s.q.ListSourcingShippingPlans(ctx, store.ListSourcingShippingPlansParams{TenantID: tenantID, RequestID: request.ID})
	if err != nil {
		return nil, err
	}
	result := make([]ShippingPlanView, 0, len(headers))
	for _, header := range headers {
		items, itemErr := s.q.ListSourcingShippingPlanItems(ctx, store.ListSourcingShippingPlanItemsParams{TenantID: tenantID, PlanID: header.ID})
		if itemErr != nil {
			return nil, itemErr
		}
		result = append(result, ShippingPlanView{Header: header, Items: items})
	}
	return result, nil
}

func (s *Service) SubmitSourcingShippingPlanToSales(ctx context.Context, tenantID, id int64, op Operator) (ShippingPlanView, error) {
	current, err := s.GetSourcingShippingPlan(ctx, tenantID, id)
	if err != nil {
		return ShippingPlanView{}, err
	}
	if current.Header.Status != "CONFIRMED" {
		return ShippingPlanView{}, apierr.Conflict("SC_SHIPPING_PLAN_SUBMITTED", "只有未提交的船运经理方案可以提交销售")
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		count, err := q.SubmitSourcingShippingPlanToSales(ctx, store.SubmitSourcingShippingPlanToSalesParams{OperatorID: &op.ID, OperatorName: op.Name, TenantID: tenantID, ID: id})
		if err != nil || count != 1 {
			return err
		}
		return q.MarkSourcingShippingPlanSubmitted(ctx, store.MarkSourcingShippingPlanSubmittedParams{TenantID: tenantID, ID: current.Header.RequestID})
	})
	if err != nil {
		return ShippingPlanView{}, err
	}
	return s.GetSourcingShippingPlan(ctx, tenantID, id)
}
