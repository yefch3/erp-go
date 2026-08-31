package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

type SalesPlanItemInput struct {
	SourcingLineID, ProcurementPlanItemID, ShippingPlanItemID int64
	OptionType                                                string
	Priority                                                  int32
	CustomerCurrency, CustomerUnitPrice                       string
	PromisedDeliveryDate, LineNote                            string
}

type SalesShippingOptionInput struct {
	ShippingPlanItemIDs                     []int64
	CustomerCurrency, CustomerFreightAmount string
	CustomerNote                            string
}

type NewSalesPlan struct {
	CaseID, ProcurementPlanID, ShippingPlanID int64
	ValidUntil, CustomerNote, InternalNote    string
	Items                                     []SalesPlanItemInput
	ShippingOptions                           []SalesShippingOptionInput
}

type SalesShippingOptionView struct {
	Header store.ListSalesShippingOptionsRow
	Lines  []store.ListSalesShippingOptionLinesRow
}

type SalesPlanView struct {
	Header          store.ListSalesPlansRow
	Items           []store.ListSalesPlanItemsRow
	ShippingOptions []SalesShippingOptionView
}

type CustomerFeedbackInput struct {
	CaseID, SalesPlanID          int64
	ContactName, Channel, Result string
	Summary, ContactedAt         string
}

type ShippingReworkInput struct {
	CaseID, SalesPlanID, SourcingLineID, ShippingOptionLineID int64
	RequestType, Reason                                       string
}

func (s *Service) requireResponsibleSales(ctx context.Context, tenantID, caseID int64, op Operator) (store.SalesNegotiationCaseRow, error) {
	row, err := s.q.SalesNegotiationCase(ctx, store.SalesNegotiationCaseParams{TenantID: tenantID, ID: caseID})
	if errors.Is(err, pgx.ErrNoRows) || err == nil && row.OwnerID != op.ID {
		return store.SalesNegotiationCaseRow{}, apierr.NotFound("SC_CASE_NOT_FOUND", "客户询盘不存在或不属于当前销售")
	}
	return row, err
}

func (s *Service) CreateSalesPlan(ctx context.Context, tenantID int64, in NewSalesPlan, op Operator) (SalesPlanView, error) {
	in.ValidUntil, in.CustomerNote, in.InternalNote = strings.TrimSpace(in.ValidUntil), strings.TrimSpace(in.CustomerNote), strings.TrimSpace(in.InternalNote)
	if in.CaseID == 0 || in.ProcurementPlanID == 0 || in.ValidUntil == "" || len(in.Items) == 0 {
		return SalesPlanView{}, apierr.Invalid("SC_SALES_PLAN_REQUIRED", "请选择采购方案、填写有效期并配置对客产品")
	}
	caseRow, err := s.requireResponsibleSales(ctx, tenantID, in.CaseID, op)
	if err != nil {
		return SalesPlanView{}, err
	}
	procurement, err := s.q.SalesPlanSubmittedProcurement(ctx, store.SalesPlanSubmittedProcurementParams{TenantID: tenantID, CaseID: in.CaseID, ID: in.ProcurementPlanID})
	if err != nil || procurement.TargetSalesID != op.ID {
		return SalesPlanView{}, apierr.Invalid("SC_SALES_PROCUREMENT_PLAN", "只能使用定向提交给你的采购经理方案")
	}
	if in.ShippingPlanID != 0 {
		shipping, shippingErr := s.q.SalesPlanSubmittedShipping(ctx, store.SalesPlanSubmittedShippingParams{TenantID: tenantID, CaseID: in.CaseID, ID: in.ShippingPlanID})
		if shippingErr != nil || shipping.TargetSalesID != op.ID {
			return SalesPlanView{}, apierr.Invalid("SC_SALES_SHIPPING_PLAN", "只能使用定向提交给你的船运经理方案")
		}
	}
	expected, err := s.q.CountSalesPlanLines(ctx, store.CountSalesPlanLinesParams{TenantID: tenantID, CaseID: in.CaseID})
	if err != nil {
		return SalesPlanView{}, err
	}
	primary, priorities, seen := map[int64]bool{}, map[string]bool{}, map[int64]bool{}
	legacyShippingIDs := map[int64]bool{}
	type checked struct {
		input             SalesPlanItemInput
		product, qty, uom string
	}
	checkedItems := make([]checked, 0, len(in.Items))
	for _, item := range in.Items {
		item.OptionType = strings.ToUpper(strings.TrimSpace(item.OptionType))
		item.CustomerCurrency = strings.ToUpper(strings.TrimSpace(item.CustomerCurrency))
		item.CustomerUnitPrice = strings.TrimSpace(item.CustomerUnitPrice)
		item.LineNote = strings.TrimSpace(item.LineNote)
		if (item.OptionType != "PRIMARY" && item.OptionType != "ALTERNATIVE") || item.Priority <= 0 || len(item.CustomerCurrency) != 3 {
			return SalesPlanView{}, apierr.Invalid("SC_SALES_PLAN_ITEM", "请填写有效的主方案/备选顺序、币种和价格")
		}
		price, priceErr := decimal.NewFromString(item.CustomerUnitPrice)
		if priceErr != nil || price.IsNegative() {
			return SalesPlanView{}, apierr.Invalid("SC_SALES_PLAN_PRICE", "对客单价必须是大于或等于 0 的数字")
		}
		candidate, candidateErr := s.q.SalesPlanProcurementCandidate(ctx, store.SalesPlanProcurementCandidateParams{TenantID: tenantID, PlanID: in.ProcurementPlanID, ID: item.ProcurementPlanItemID})
		if candidateErr != nil || candidate.SourcingLineID != item.SourcingLineID {
			return SalesPlanView{}, apierr.Invalid("SC_SALES_PLAN_PROCUREMENT_ITEM", "采购报价不属于所选经理方案或产品")
		}
		if item.ShippingPlanItemID != 0 {
			if in.ShippingPlanID == 0 {
				return SalesPlanView{}, apierr.Invalid("SC_SALES_PLAN_SHIPPING_REQUIRED", "选择船运报价前必须选择船运经理方案")
			}
			ship, shipErr := s.q.SalesPlanShippingCandidate(ctx, store.SalesPlanShippingCandidateParams{TenantID: tenantID, PlanID: in.ShippingPlanID, ID: item.ShippingPlanItemID})
			if shipErr != nil || ship.SourcingLineID != item.SourcingLineID {
				return SalesPlanView{}, apierr.Invalid("SC_SALES_PLAN_SHIPPING_ITEM", "船运报价不属于所选经理方案或产品")
			}
			legacyShippingIDs[item.ShippingPlanItemID] = true
		}
		key := strings.Join([]string{strconv.FormatInt(item.SourcingLineID, 10), item.OptionType, strconv.Itoa(int(item.Priority))}, ":")
		if priorities[key] || seen[item.ProcurementPlanItemID] {
			return SalesPlanView{}, apierr.Invalid("SC_SALES_PLAN_DUPLICATE", "同一产品的方案顺序或供应商候选不能重复")
		}
		priorities[key], seen[item.ProcurementPlanItemID] = true, true
		if item.OptionType == "PRIMARY" {
			primary[item.SourcingLineID] = true
		}
		checkedItems = append(checkedItems, checked{item, candidate.ProductName, candidate.PiAvailableQty, candidate.UomCode})
	}
	if int64(len(primary)) != expected {
		return SalesPlanView{}, apierr.Invalid("SC_SALES_PLAN_INCOMPLETE", "每个有效产品至少需要一个主方案")
	}
	// Compatibility for T7 callers: old rows paired a shipping line with every
	// procurement candidate. Collapse those line IDs into shipment-level
	// customer options instead of multiplying the freight per product.
	if len(in.ShippingOptions) == 0 && len(legacyShippingIDs) > 0 {
		byOption := map[int64]*SalesShippingOptionInput{}
		for id := range legacyShippingIDs {
			candidate, candidateErr := s.q.SalesPlanShippingCandidate(ctx, store.SalesPlanShippingCandidateParams{TenantID: tenantID, PlanID: in.ShippingPlanID, ID: id})
			if candidateErr != nil {
				return SalesPlanView{}, apierr.Invalid("SC_SALES_PLAN_SHIPPING_ITEM", "船运候选已失效")
			}
			group := byOption[candidate.OptionID]
			if group == nil {
				group = &SalesShippingOptionInput{CustomerCurrency: candidate.Currency, CustomerFreightAmount: candidate.SiTotalFreight}
				byOption[candidate.OptionID] = group
			}
			group.ShippingPlanItemIDs = append(group.ShippingPlanItemIDs, id)
		}
		for _, group := range byOption {
			in.ShippingOptions = append(in.ShippingOptions, *group)
		}
	}
	type checkedShipping struct {
		input SalesShippingOptionInput
		head  store.SalesPlanShippingCandidateRow
		lines []store.SalesPlanShippingCandidateRow
	}
	checkedShippingOptions := make([]checkedShipping, 0, len(in.ShippingOptions))
	seenShippingOptions := map[int64]bool{}
	for _, option := range in.ShippingOptions {
		option.CustomerCurrency = strings.ToUpper(strings.TrimSpace(option.CustomerCurrency))
		option.CustomerFreightAmount = strings.TrimSpace(option.CustomerFreightAmount)
		option.CustomerNote = strings.TrimSpace(option.CustomerNote)
		amount, amountErr := decimal.NewFromString(option.CustomerFreightAmount)
		if in.ShippingPlanID == 0 || len(option.ShippingPlanItemIDs) == 0 || len(option.CustomerCurrency) != 3 || amountErr != nil || amount.IsNegative() {
			return SalesPlanView{}, apierr.Invalid("SC_SALES_SHIPPING_OPTION", "请为船运候选填写有效的覆盖货物、币种和对客运费")
		}
		checked := checkedShipping{input: option}
		seenLines := map[int64]bool{}
		for _, itemID := range option.ShippingPlanItemIDs {
			candidate, candidateErr := s.q.SalesPlanShippingCandidate(ctx, store.SalesPlanShippingCandidateParams{TenantID: tenantID, PlanID: in.ShippingPlanID, ID: itemID})
			if candidateErr != nil {
				return SalesPlanView{}, apierr.Invalid("SC_SALES_PLAN_SHIPPING_ITEM", "船运报价不属于所选经理候选清单")
			}
			if checked.head.OptionID == 0 {
				checked.head = candidate
			} else if checked.head.OptionID != candidate.OptionID {
				return SalesPlanView{}, apierr.Invalid("SC_SALES_SHIPPING_MIXED", "一条客户船运候选只能来自同一家公司的同一份整票报价")
			}
			if seenLines[candidate.SourcingLineID] {
				return SalesPlanView{}, apierr.Invalid("SC_SALES_SHIPPING_DUPLICATE_LINE", "船运候选中存在重复货物")
			}
			seenLines[candidate.SourcingLineID] = true
			checked.lines = append(checked.lines, candidate)
		}
		if seenShippingOptions[checked.head.OptionID] {
			return SalesPlanView{}, apierr.Invalid("SC_SALES_SHIPPING_DUPLICATE", "同一整票船运报价只能作为一个客户候选")
		}
		seenShippingOptions[checked.head.OptionID] = true
		checkedShippingOptions = append(checkedShippingOptions, checked)
	}
	var planID int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if invalidateErr := q.InvalidateActiveCustomerSelections(ctx, store.InvalidateActiveCustomerSelectionsParams{Reason: "销售生成了新的客户沟通方案", TenantID: tenantID, CaseID: in.CaseID}); invalidateErr != nil {
			return invalidateErr
		}
		if err := q.SupersedePresentedSalesPlans(ctx, store.SupersedePresentedSalesPlansParams{TenantID: tenantID, CaseID: in.CaseID}); err != nil {
			return err
		}
		id, createErr := q.CreateSalesPlan(ctx, store.CreateSalesPlanParams{TenantID: tenantID, CaseID: in.CaseID, RequirementVersionNo: caseRow.RequirementVersionNo, ProcurementPlanID: in.ProcurementPlanID, ShippingPlanID: in.ShippingPlanID, ValidUntil: in.ValidUntil, CustomerNote: in.CustomerNote, InternalNote: in.InternalNote, CreatedBy: op.ID, CreatedByName: op.Name})
		if createErr != nil {
			return createErr
		}
		planID = id
		for _, item := range checkedItems {
			if err := q.CreateSalesPlanItem(ctx, store.CreateSalesPlanItemParams{TenantID: tenantID, PlanID: planID, SourcingLineID: item.input.SourcingLineID, ProcurementPlanItemID: item.input.ProcurementPlanItemID, OptionType: item.input.OptionType, Priority: item.input.Priority, ProductName: item.product, QuotedQty: item.qty, UomCode: item.uom, CustomerCurrency: item.input.CustomerCurrency, CustomerUnitPrice: item.input.CustomerUnitPrice, PromisedDeliveryDate: item.input.PromisedDeliveryDate, LineNote: item.input.LineNote}); err != nil {
				return err
			}
		}
		for _, option := range checkedShippingOptions {
			head := option.head
			shippingOptionID, createShippingErr := q.CreateSalesShippingOption(ctx, store.CreateSalesShippingOptionParams{TenantID: tenantID, PlanID: planID, ShippingOptionID: head.OptionID, CarrierForwarder: head.CarrierForwarder, ServiceOptionName: head.ServiceOptionName, ShippingEmployeeID: head.ShippingEmployeeID, ShippingEmployeeName: head.ShippingEmployeeName, CustomerCurrency: option.input.CustomerCurrency, CustomerFreightAmount: option.input.CustomerFreightAmount, ChargeBasis: head.ChargeBasis, PortOfLoading: head.PortOfLoading, PortOfDischarge: head.PortOfDischarge, EstimatedDeparture: head.EstimatedDeparture, EstimatedArrival: head.EstimatedArrival, ValidUntil: head.ValidUntil, CustomerNote: option.input.CustomerNote})
			if createShippingErr != nil {
				return createShippingErr
			}
			for _, line := range option.lines {
				if lineErr := q.CreateSalesShippingOptionLine(ctx, store.CreateSalesShippingOptionLineParams{TenantID: tenantID, SalesShippingOptionID: shippingOptionID, SourcingLineID: line.SourcingLineID, ShippingPlanItemID: line.ID, ShippingOptionLineID: line.ShippingOptionLineID, ProductName: line.ProductName, QuotedQty: line.QuotedQty, UomCode: line.UomCode}); lineErr != nil {
					return lineErr
				}
			}
		}
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: in.CaseID, Section: "SALES_NEGOTIATION", Action: "SALES_PLAN_PRESENTED", Summary: "销售生成客户沟通方案", BeforeJson: []byte(`{}`), AfterJson: []byte(`{}`), OperatorID: op.ID, OperatorName: op.Name})
	})
	if err != nil {
		return SalesPlanView{}, err
	}
	return s.GetSalesPlan(ctx, tenantID, planID)
}

func (s *Service) GetSalesPlan(ctx context.Context, tenantID, id int64) (SalesPlanView, error) {
	header, err := s.q.GetSalesPlan(ctx, store.GetSalesPlanParams{TenantID: tenantID, ID: id})
	if err != nil {
		return SalesPlanView{}, err
	}
	items, err := s.q.ListSalesPlanItems(ctx, store.ListSalesPlanItemsParams{TenantID: tenantID, PlanID: id})
	if err != nil {
		return SalesPlanView{}, err
	}
	shippingOptions, err := s.listSalesShippingOptions(ctx, tenantID, id)
	return SalesPlanView{Header: store.ListSalesPlansRow(header), Items: items, ShippingOptions: shippingOptions}, err
}

func (s *Service) listSalesShippingOptions(ctx context.Context, tenantID, planID int64) ([]SalesShippingOptionView, error) {
	headers, err := s.q.ListSalesShippingOptions(ctx, store.ListSalesShippingOptionsParams{TenantID: tenantID, PlanID: planID})
	if err != nil {
		return nil, err
	}
	out := make([]SalesShippingOptionView, 0, len(headers))
	for _, header := range headers {
		lines, lineErr := s.q.ListSalesShippingOptionLines(ctx, store.ListSalesShippingOptionLinesParams{TenantID: tenantID, SalesShippingOptionID: header.ID})
		if lineErr != nil {
			return nil, lineErr
		}
		out = append(out, SalesShippingOptionView{Header: header, Lines: lines})
	}
	return out, nil
}

func (s *Service) ListSalesPlans(ctx context.Context, tenantID, caseID int64, op Operator) ([]SalesPlanView, error) {
	if _, err := s.requireResponsibleSales(ctx, tenantID, caseID, op); err != nil {
		return nil, err
	}
	headers, err := s.q.ListSalesPlans(ctx, store.ListSalesPlansParams{TenantID: tenantID, CaseID: caseID})
	if err != nil {
		return nil, err
	}
	out := make([]SalesPlanView, 0, len(headers))
	for _, h := range headers {
		items, e := s.q.ListSalesPlanItems(ctx, store.ListSalesPlanItemsParams{TenantID: tenantID, PlanID: h.ID})
		if e != nil {
			return nil, e
		}
		shippingOptions, e := s.listSalesShippingOptions(ctx, tenantID, h.ID)
		if e != nil {
			return nil, e
		}
		out = append(out, SalesPlanView{Header: h, Items: items, ShippingOptions: shippingOptions})
	}
	return out, nil
}

func (s *Service) AddCustomerFeedback(ctx context.Context, tenantID int64, in CustomerFeedbackInput, op Operator) ([]store.ListCustomerFeedbackRow, error) {
	in.ContactName, in.Channel, in.Result, in.Summary, in.ContactedAt = strings.TrimSpace(in.ContactName), strings.ToUpper(strings.TrimSpace(in.Channel)), strings.ToUpper(strings.TrimSpace(in.Result)), strings.TrimSpace(in.Summary), strings.TrimSpace(in.ContactedAt)
	if _, err := s.requireResponsibleSales(ctx, tenantID, in.CaseID, op); err != nil {
		return nil, err
	}
	if in.SalesPlanID == 0 || in.Summary == "" || in.ContactedAt == "" {
		return nil, apierr.Invalid("SC_CUSTOMER_FEEDBACK_REQUIRED", "请选择销售方案并填写沟通时间和反馈摘要")
	}
	if _, err := time.Parse(time.RFC3339, in.ContactedAt); err != nil {
		return nil, apierr.Invalid("SC_CUSTOMER_FEEDBACK_TIME", "沟通时间格式无效")
	}
	if in.Channel != "PHONE" && in.Channel != "EMAIL" && in.Channel != "MEETING" && in.Channel != "OTHER" {
		return nil, apierr.Invalid("SC_CUSTOMER_FEEDBACK_CHANNEL", "请选择沟通方式")
	}
	if in.Result != "CONTINUE_NEGOTIATION" && in.Result != "REQUOTE_REQUIRED" && in.Result != "COMMENT" {
		return nil, apierr.Invalid("SC_CUSTOMER_FEEDBACK_RESULT", "请选择客户反馈结果")
	}
	plan, err := s.q.GetSalesPlan(ctx, store.GetSalesPlanParams{TenantID: tenantID, ID: in.SalesPlanID})
	if err != nil || plan.CaseID != in.CaseID {
		return nil, apierr.Invalid("SC_CUSTOMER_FEEDBACK_PLAN", "销售方案不属于当前询盘")
	}
	_, err = s.q.CreateCustomerFeedback(ctx, store.CreateCustomerFeedbackParams{TenantID: tenantID, CaseID: in.CaseID, SalesPlanID: in.SalesPlanID, ContactName: in.ContactName, Channel: in.Channel, Result: in.Result, Summary: in.Summary, ContactedAt: in.ContactedAt, CreatedBy: op.ID, CreatedByName: op.Name})
	if err != nil {
		return nil, err
	}
	_ = s.q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: in.CaseID, Section: "SALES_NEGOTIATION", Action: "CUSTOMER_FEEDBACK_RECORDED", Summary: "销售登记客户反馈", BeforeJson: []byte(`{}`), AfterJson: []byte(`{}`), Reason: in.Summary, OperatorID: op.ID, OperatorName: op.Name})
	return s.q.ListCustomerFeedback(ctx, store.ListCustomerFeedbackParams{TenantID: tenantID, CaseID: in.CaseID})
}

func (s *Service) ListCustomerFeedback(ctx context.Context, tenantID, caseID int64, op Operator) ([]store.ListCustomerFeedbackRow, error) {
	if _, err := s.requireResponsibleSales(ctx, tenantID, caseID, op); err != nil {
		return nil, err
	}
	return s.q.ListCustomerFeedback(ctx, store.ListCustomerFeedbackParams{TenantID: tenantID, CaseID: caseID})
}

func (s *Service) CreateSalesProcurementRework(ctx context.Context, tenantID int64, salesPlanID int64, in NewProcurementRework, op Operator) (store.ListProcurementReworkRequestsRow, error) {
	plan, err := s.q.GetSalesPlan(ctx, store.GetSalesPlanParams{TenantID: tenantID, ID: salesPlanID})
	if err != nil {
		return store.ListProcurementReworkRequestsRow{}, err
	}
	if _, err = s.requireResponsibleSales(ctx, tenantID, plan.CaseID, op); err != nil {
		return store.ListProcurementReworkRequestsRow{}, err
	}
	in.CaseID, in.PlanID = plan.CaseID, plan.ProcurementPlanID
	row, err := s.CreateProcurementRework(ctx, tenantID, in, op)
	if err == nil {
		_ = s.q.MarkSalesPlanReworkRequested(ctx, store.MarkSalesPlanReworkRequestedParams{TenantID: tenantID, ID: salesPlanID})
	}
	return row, err
}

func (s *Service) CreateShippingRework(ctx context.Context, tenantID int64, in ShippingReworkInput, op Operator) (store.ListShippingReworksRow, error) {
	in.RequestType, in.Reason = strings.ToUpper(strings.TrimSpace(in.RequestType)), strings.TrimSpace(in.Reason)
	if _, err := s.requireResponsibleSales(ctx, tenantID, in.CaseID, op); err != nil {
		return store.ListShippingReworksRow{}, err
	}
	if in.Reason == "" || (in.RequestType != "REQUOTE" && in.RequestType != "RENEGOTIATE" && in.RequestType != "ADD_CARRIER") {
		return store.ListShippingReworksRow{}, apierr.Invalid("SC_SHIPPING_REWORK_REQUIRED", "请选择船运退回范围并填写原因")
	}
	if in.SalesPlanID != 0 {
		plan, planErr := s.q.GetSalesPlan(ctx, store.GetSalesPlanParams{TenantID: tenantID, ID: in.SalesPlanID})
		if planErr != nil || plan.CaseID != in.CaseID {
			return store.ListShippingReworksRow{}, apierr.Invalid("SC_SHIPPING_REWORK_PLAN", "销售方案不属于当前询盘")
		}
	}
	args := store.CreateShippingReworkParams{TenantID: tenantID, CaseID: in.CaseID, SalesPlanID: in.SalesPlanID, SourcingLineID: in.SourcingLineID, ShippingOptionLineID: in.ShippingOptionLineID, RequestType: in.RequestType, Reason: in.Reason, CreatedBy: op.ID, CreatedByName: op.Name}
	if in.ShippingOptionLineID != 0 {
		q, e := s.q.ShippingReworkQuote(ctx, store.ShippingReworkQuoteParams{TenantID: tenantID, CaseID: in.CaseID, ID: in.ShippingOptionLineID})
		if e != nil {
			return store.ListShippingReworksRow{}, apierr.Invalid("SC_SHIPPING_REWORK_QUOTE", "船运报价不存在")
		}
		args.SourcingLineID, args.ScopeType, args.AssignedShippingID, args.AssignedShippingName, args.CarrierForwarder, args.ProductName = q.SourcingLineID, "QUOTE", q.ShippingID, q.ShippingName, q.CarrierForwarder, q.ProductName
	} else {
		args.ScopeType = "ALL"
		if in.SourcingLineID != 0 {
			p, e := s.q.ShippingReworkProduct(ctx, store.ShippingReworkProductParams{TenantID: tenantID, CaseID: in.CaseID, ID: in.SourcingLineID})
			if e != nil {
				return store.ListShippingReworksRow{}, apierr.Invalid("SC_SHIPPING_REWORK_PRODUCT", "产品不存在")
			}
			args.ScopeType, args.ProductName = "PRODUCT", p.Product
		}
	}
	id, err := s.q.CreateShippingRework(ctx, args)
	if err != nil {
		return store.ListShippingReworksRow{}, err
	}
	if in.SalesPlanID != 0 {
		_ = s.q.MarkSalesPlanReworkRequested(ctx, store.MarkSalesPlanReworkRequestedParams{TenantID: tenantID, ID: in.SalesPlanID})
	}
	_ = s.q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: in.CaseID, Section: "SALES_NEGOTIATION", Action: "SHIPPING_REWORK_REQUESTED", Summary: "销售退回船运询价", BeforeJson: []byte(`{}`), AfterJson: []byte(`{}`), Reason: in.Reason, OperatorID: op.ID, OperatorName: op.Name})
	rows, err := s.q.ListShippingReworks(ctx, store.ListShippingReworksParams{TenantID: tenantID, CaseID: in.CaseID})
	if err != nil {
		return store.ListShippingReworksRow{}, err
	}
	for _, r := range rows {
		if r.ID == id {
			return r, nil
		}
	}
	return store.ListShippingReworksRow{}, apierr.NotFound("SC_SHIPPING_REWORK_NOT_FOUND", "船运退回任务不存在")
}

func (s *Service) ListShippingReworks(ctx context.Context, tenantID, caseID int64, op Operator) ([]store.ListShippingReworksRow, error) {
	if _, err := s.requireResponsibleSales(ctx, tenantID, caseID, op); err != nil {
		return nil, err
	}
	return s.q.ListShippingReworks(ctx, store.ListShippingReworksParams{TenantID: tenantID, CaseID: caseID})
}

func (s *Service) ListMyShippingReworks(ctx context.Context, tenantID int64, op Operator) ([]store.ListMyShippingReworksRow, error) {
	return s.q.ListMyShippingReworks(ctx, store.ListMyShippingReworksParams{TenantID: tenantID, EmployeeID: &op.ID})
}

func (s *Service) ResolveShippingRework(ctx context.Context, tenantID, id int64, note string, op Operator) error {
	note = strings.TrimSpace(note)
	if note == "" {
		return apierr.Invalid("SC_SHIPPING_REWORK_RESULT", "请填写处理结果")
	}
	row, err := s.q.GetShippingRework(ctx, store.GetShippingReworkParams{TenantID: tenantID, ID: id})
	if err != nil {
		return apierr.NotFound("SC_SHIPPING_REWORK_NOT_FOUND", "船运退回任务不存在")
	}
	if row.AssignedShippingID != 0 && row.AssignedShippingID != op.ID {
		return apierr.Permission("SC_SHIPPING_REWORK_ASSIGNEE", "该任务已指定给原船运报价人")
	}
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		count, resolveErr := q.ResolveShippingRework(ctx, store.ResolveShippingReworkParams{OperatorID: &op.ID, OperatorName: op.Name, ResolutionNote: note, TenantID: tenantID, ID: id})
		if resolveErr != nil {
			return resolveErr
		}
		if count == 0 {
			return apierr.Conflict("SC_SHIPPING_REWORK_RESOLVED", "船运退回任务已经处理")
		}
		if resolveErr = q.ResolveFinalTaskByShippingRework(ctx, store.ResolveFinalTaskByShippingReworkParams{TenantID: tenantID, ShippingReworkID: &id}); resolveErr != nil {
			return resolveErr
		}
		if resolveErr = q.CompleteReadyCustomerSelections(ctx, tenantID); resolveErr != nil {
			return resolveErr
		}
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: row.CaseID, Section: "SALES_NEGOTIATION", Action: "SHIPPING_REWORK_RESOLVED", Summary: "船运人员完成销售退回任务", BeforeJson: []byte(`{}`), AfterJson: []byte(`{}`), Reason: note, OperatorID: op.ID, OperatorName: op.Name})
	})
}
