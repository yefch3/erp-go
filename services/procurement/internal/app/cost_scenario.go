package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

type CostSelectionInput struct{ SourcingLineID, SupplierQuoteLineID int64 }
type CostChargeInput struct {
	ChargeType, Basis, Description, OriginPort, DestinationPort, ContainerType string
	Amount, Currency, EffectiveAt, ValidUntil, Source, Remark                  string
}
type NewCostScenario struct {
	CaseID                                             int64
	Currency, AllocationBasis, MarginType, MarginValue string
	Selections                                         []CostSelectionInput
	Charges                                            []CostChargeInput
}
type CostScenarioView struct {
	Header  store.GetCostScenarioRow
	Charges []store.ListCostChargesRow
	Lines   []store.ListCostScenarioLinesRow
}
type costLine struct {
	row                                                             store.CostScenarioCandidateRow
	qty, sourcePrice, sourceRate, productCost                       decimal.Decimal
	allocated, landedUnit, marginUnit, customerUnit, customerAmount decimal.Decimal
}
type costCharge struct {
	in                            CostChargeInput
	amount, converted, sourceRate decimal.Decimal
	rate                          Rate
}

func convertAmount(amount, sourceRate, targetRate decimal.Decimal) decimal.Decimal {
	return amount.Div(sourceRate).Mul(targetRate)
}

// CreateCostScenario 根据已核实的工厂报价创建新的成本版本，并保留原币、汇率、费用和利润快照。
func (s *Service) CreateCostScenario(ctx context.Context, tenantID int64, in NewCostScenario, op Operator) (CostScenarioView, error) {
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.AllocationBasis = strings.ToUpper(in.AllocationBasis)
	in.MarginType = strings.ToUpper(in.MarginType)
	if in.CaseID == 0 || in.Currency == "" || len(in.Selections) == 0 {
		return CostScenarioView{}, apierr.Invalid("SC_COST_REQUIRED", "请选择报价明细和方案币种")
	}
	if in.AllocationBasis != "TONS" && in.AllocationBasis != "PRODUCT_AMOUNT" {
		return CostScenarioView{}, apierr.Invalid("SC_ALLOCATION_INVALID", "费用分摊基准无效")
	}
	if in.MarginType != "FIXED_PER_TON" && in.MarginType != "PERCENT" {
		return CostScenarioView{}, apierr.Invalid("SC_MARGIN_INVALID", "利润规则无效")
	}
	margin, err := decimal.NewFromString(in.MarginValue)
	if err != nil || margin.IsNegative() {
		return CostScenarioView{}, apierr.Invalid("SC_MARGIN_INVALID", "利润值不能为负数")
	}
	caseRow, err := s.q.CostScenarioCase(ctx, store.CostScenarioCaseParams{TenantID: tenantID, ID: in.CaseID})
	if err != nil {
		return CostScenarioView{}, apierr.NotFound("SC_CASE_NOT_FOUND", "询价案件不存在")
	}
	if caseRow.Status != "QUOTES_RECEIVED" && caseRow.Status != "COSTING" {
		return CostScenarioView{}, apierr.Conflict("SC_QUOTES_REQUIRED", "请先收齐供应商报价")
	}
	if caseRow.HandoffStatus != "IN_PROGRESS" {
		return CostScenarioView{}, apierr.Conflict("SC_HANDOFF_INACTIVE", "当前寻源任务不在采购处理中")
	}
	expected, err := s.q.CountConfirmedSourcingLines(ctx, store.CountConfirmedSourcingLinesParams{TenantID: tenantID, CaseID: in.CaseID})
	if err != nil {
		return CostScenarioView{}, err
	}
	if int64(len(in.Selections)) != expected {
		return CostScenarioView{}, apierr.Invalid("SC_COST_INCOMPLETE", "每条有效询盘明细必须选择一个供应商报价")
	}
	targetRate, err := s.rates.Latest(ctx, in.Currency)
	if err != nil {
		return CostScenarioView{}, err
	}
	rateCache := map[string]Rate{in.Currency: targetRate}
	lines := make([]costLine, 0, len(in.Selections))
	seen := map[int64]bool{}
	totalQty, productTotal := decimal.Zero, decimal.Zero
	for _, selection := range in.Selections {
		row, err := s.q.CostScenarioCandidate(ctx, store.CostScenarioCandidateParams{TenantID: tenantID, CaseID: in.CaseID, ID: selection.SupplierQuoteLineID})
		if err != nil || row.SourcingLineID != selection.SourcingLineID || seen[row.SourcingLineID] {
			return CostScenarioView{}, apierr.Invalid("SC_COST_SELECTION_INVALID", "所选供应商报价与询盘明细不一致")
		}
		seen[row.SourcingLineID] = true
		qty, qerr := decimal.NewFromString(row.QlQty)
		price, perr := decimal.NewFromString(row.QlUnitPrice)
		if qerr != nil || perr != nil || qty.LessThanOrEqual(decimal.Zero) || price.IsNegative() {
			return CostScenarioView{}, apierr.Invalid("SC_COST_PRICE_INVALID", "供应商报价数量或单价无效")
		}
		rate, ok := rateCache[row.Currency]
		if !ok {
			rate, err = s.rates.Latest(ctx, row.Currency)
			if err != nil {
				return CostScenarioView{}, err
			}
			rateCache[row.Currency] = rate
		}
		convertedUnit := convertAmount(price, rate.Rate, targetRate.Rate)
		productCost := qty.Mul(convertedUnit).Round(2)
		lines = append(lines, costLine{row: row, qty: qty, sourcePrice: price, sourceRate: rate.Rate, productCost: productCost})
		totalQty = totalQty.Add(qty)
		productTotal = productTotal.Add(productCost)
	}
	charges := make([]costCharge, 0, len(in.Charges))
	chargeTotal := decimal.Zero
	for _, input := range in.Charges {
		input.ChargeType = strings.ToUpper(input.ChargeType)
		input.Basis = strings.ToUpper(input.Basis)
		input.Currency = strings.ToUpper(input.Currency)
		if !validChargeType(input.ChargeType) || !validChargeBasis(input.Basis) {
			return CostScenarioView{}, apierr.Invalid("SC_CHARGE_TYPE_INVALID", "费用类型或计费方式无效")
		}
		amount, aerr := decimal.NewFromString(input.Amount)
		if aerr != nil || amount.IsNegative() || input.Currency == "" {
			return CostScenarioView{}, apierr.Invalid("SC_CHARGE_AMOUNT_INVALID", "费用金额或币种无效")
		}
		rate, ok := rateCache[input.Currency]
		if !ok {
			rate, err = s.rates.Latest(ctx, input.Currency)
			if err != nil {
				return CostScenarioView{}, err
			}
			rateCache[input.Currency] = rate
		}
		converted := convertAmount(amount, rate.Rate, targetRate.Rate)
		if input.Basis == "PER_TON" {
			converted = converted.Mul(totalQty)
		}
		converted = converted.Round(2)
		charges = append(charges, costCharge{in: input, amount: amount, converted: converted, sourceRate: rate.Rate, rate: rate})
		chargeTotal = chargeTotal.Add(converted)
	}
	allocateCharges(lines, chargeTotal, in.AllocationBasis, totalQty, productTotal)
	landedTotal, marginTotal, customerTotal := decimal.Zero, decimal.Zero, decimal.Zero
	for i := range lines {
		lines[i].landedUnit = lines[i].productCost.Add(lines[i].allocated).Div(lines[i].qty)
		if in.MarginType == "PERCENT" {
			lines[i].marginUnit = lines[i].landedUnit.Mul(margin).Div(decimal.NewFromInt(100))
		} else {
			lines[i].marginUnit = margin
		}
		lines[i].customerUnit = lines[i].landedUnit.Add(lines[i].marginUnit).Round(4)
		lines[i].customerAmount = lines[i].customerUnit.Mul(lines[i].qty).Round(2)
		landedTotal = landedTotal.Add(lines[i].productCost).Add(lines[i].allocated)
		marginTotal = marginTotal.Add(lines[i].customerAmount.Sub(lines[i].productCost).Sub(lines[i].allocated))
		customerTotal = customerTotal.Add(lines[i].customerAmount)
	}
	var scenarioID int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// 同一案件串行分配成本版本号，避免两名采购员同时建方案都拿到 V2。
		if _, e := q.LockCostScenarioCase(ctx, store.LockCostScenarioCaseParams{TenantID: tenantID, ID: in.CaseID}); e != nil {
			return e
		}
		head, e := q.CreateCostScenario(ctx, store.CreateCostScenarioParams{TenantID: tenantID, CaseID: in.CaseID, RequirementVersionNo: caseRow.RequirementVersionNo, Currency: in.Currency, AllocationBasis: in.AllocationBasis, MarginType: in.MarginType, MarginValue: margin.String(), FxRate: targetRate.Rate.String(), FxRateAt: pgtype.Timestamptz{Time: targetRate.At, Valid: true}, FxSource: targetRate.Source, FxBaseCurrency: targetRate.Base, ProductTotal: productTotal.StringFixed(2), ChargeTotal: chargeTotal.StringFixed(2), LandedTotal: landedTotal.StringFixed(2), MarginTotal: marginTotal.StringFixed(2), CustomerTotal: customerTotal.StringFixed(2), CreatedBy: op.ID, CreatedByName: op.Name})
		if e != nil {
			return e
		}
		scenarioID = head.ID
		for _, c := range charges {
			if e = q.CreateCostCharge(ctx, store.CreateCostChargeParams{TenantID: tenantID, ScenarioID: scenarioID, ChargeType: c.in.ChargeType, Basis: c.in.Basis, Description: c.in.Description, OriginPort: c.in.OriginPort, DestinationPort: c.in.DestinationPort, ContainerType: c.in.ContainerType, Amount: c.amount.String(), Currency: c.in.Currency, ConvertedAmount: c.converted.StringFixed(2), SourceFxRate: c.sourceRate.String(), TargetFxRate: targetRate.Rate.String(), FxRateAt: pgtype.Timestamptz{Time: c.rate.At, Valid: true}, FxSource: c.rate.Source, EffectiveAt: c.in.EffectiveAt, ValidUntil: c.in.ValidUntil, Source: c.in.Source, Remark: c.in.Remark}); e != nil {
				return e
			}
		}
		for _, l := range lines {
			if e = q.CreateCostScenarioLine(ctx, store.CreateCostScenarioLineParams{TenantID: tenantID, ScenarioID: scenarioID, SourcingLineID: l.row.SourcingLineID, SupplierQuoteLineID: l.row.QuoteLineID, SupplierName: l.row.SupplierName, ProductID: l.row.ProductID, SkuID: l.row.SkuID, ProductName: l.row.Product, SpecSnapshot: l.row.SpecSnapshot, Qty: l.qty.String(), UomCode: l.row.UomCode, SourceCurrency: l.row.Currency, SourceUnitPrice: l.sourcePrice.String(), SourceFxRate: l.sourceRate.String(), TargetFxRate: targetRate.Rate.String(), ProductCost: l.productCost.StringFixed(2), AllocatedCharge: l.allocated.StringFixed(2), LandedCost: l.landedUnit.StringFixed(6), MarginAmount: l.marginUnit.StringFixed(6), CustomerUnitPrice: l.customerUnit.StringFixed(4), CustomerAmount: l.customerAmount.StringFixed(2)}); e != nil {
				return e
			}
		}
		return q.MarkSourcingCaseCosting(ctx, store.MarkSourcingCaseCostingParams{TenantID: tenantID, ID: in.CaseID})
	})
	if err != nil {
		return CostScenarioView{}, err
	}
	return s.GetCostScenario(ctx, tenantID, scenarioID)
}

func allocateCharges(lines []costLine, total decimal.Decimal, basis string, totalQty, productTotal decimal.Decimal) {
	allocated := decimal.Zero
	for i := range lines {
		if i == len(lines)-1 {
			lines[i].allocated = total.Sub(allocated)
			break
		}
		weight := lines[i].qty
		denom := totalQty
		if basis == "PRODUCT_AMOUNT" {
			weight = lines[i].productCost
			denom = productTotal
		}
		if denom.IsZero() {
			lines[i].allocated = decimal.Zero
		} else {
			lines[i].allocated = total.Mul(weight).Div(denom).Round(2)
		}
		allocated = allocated.Add(lines[i].allocated)
	}
}
func validChargeType(v string) bool {
	switch v {
	case "ORIGIN_TERMINAL", "DESTINATION_TERMINAL", "OCEAN_FREIGHT", "INSURANCE", "DOCUMENT", "FINANCE", "OTHER":
		return true
	}
	return false
}
func validChargeBasis(v string) bool {
	switch v {
	case "PER_TON", "PER_CONTAINER", "PER_SHIPMENT", "FIXED":
		return true
	}
	return false
}

// ListCostScenarios 按询价项目列出全部成本版本，供员工回看每次报价调整。
func (s *Service) ListCostScenarios(ctx context.Context, tenantID, caseID int64) ([]store.ListCostScenariosRow, error) {
	if _, e := s.q.CostScenarioCase(ctx, store.CostScenarioCaseParams{TenantID: tenantID, ID: caseID}); e != nil {
		return nil, apierr.NotFound("SC_CASE_NOT_FOUND", "询价案件不存在")
	}
	return s.q.ListCostScenarios(ctx, store.ListCostScenariosParams{TenantID: tenantID, CaseID: caseID})
}

// GetCostScenario 返回指定成本版本及其费用、产品成本明细。
func (s *Service) GetCostScenario(ctx context.Context, tenantID, id int64) (CostScenarioView, error) {
	h, e := s.q.GetCostScenario(ctx, store.GetCostScenarioParams{TenantID: tenantID, ID: id})
	if e != nil {
		return CostScenarioView{}, apierr.NotFound("SC_COST_NOT_FOUND", "成本方案不存在")
	}
	c, e := s.q.ListCostCharges(ctx, store.ListCostChargesParams{TenantID: tenantID, ScenarioID: id})
	if e != nil {
		return CostScenarioView{}, e
	}
	l, e := s.q.ListCostScenarioLines(ctx, store.ListCostScenarioLinesParams{TenantID: tenantID, ScenarioID: id})
	return CostScenarioView{Header: h, Charges: c, Lines: l}, e
}

// ConfirmCostScenario 确认本次工厂报价组合并记录中标原因，同时让此前生效的成本版本失效。
// 中标原因必填，因为最终选择可能综合 MOQ、交期、合作关系等因素，而不一定是最低价。
func (s *Service) ConfirmCostScenario(ctx context.Context, tenantID, id int64, reason string, op Operator) (CostScenarioView, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return CostScenarioView{}, apierr.Invalid("SC_COST_REASON_REQUIRED", "请填写选择该方案的原因——中标依据要留在纸上")
	}
	current, e := s.GetCostScenario(ctx, tenantID, id)
	if e != nil {
		return CostScenarioView{}, e
	}
	if current.Header.Status != "DRAFT" {
		return CostScenarioView{}, apierr.Conflict("SC_COST_NOT_DRAFT", "只有草稿成本方案可以确认")
	}
	caseRow, e := s.q.CostScenarioCase(ctx, store.CostScenarioCaseParams{TenantID: tenantID, ID: current.Header.CaseID})
	if e != nil {
		return CostScenarioView{}, e
	}
	if caseRow.Status != "QUOTES_RECEIVED" && caseRow.Status != "COSTING" {
		return CostScenarioView{}, apierr.Conflict("SC_COST_QUOTE_ACTIVE", "当前已有客户报价在处理；只有客户明确拒绝后才能确认新成本版本")
	}
	e = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if e := q.SupersedeOtherCostScenarios(ctx, store.SupersedeOtherCostScenariosParams{TenantID: tenantID, CaseID: current.Header.CaseID, ID: id}); e != nil {
			return e
		}
		n, e := q.ConfirmCostScenario(ctx, store.ConfirmCostScenarioParams{ConfirmedBy: &op.ID, ConfirmedByName: op.Name, ConfirmReason: reason, TenantID: tenantID, ID: id})
		if e != nil {
			return e
		}
		if n != 1 {
			return apierr.Conflict("SC_COST_NOT_DRAFT", "成本方案状态已改变")
		}
		if e := q.MarkSourcingCaseCostConfirmed(ctx, store.MarkSourcingCaseCostConfirmedParams{TenantID: tenantID, ID: current.Header.CaseID}); e != nil {
			return e
		}
		afterJSON, _ := json.Marshal(map[string]string{"scenarioNo": current.Header.ScenarioNo, "reason": reason})
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: current.Header.CaseID,
			Section: "COST", Action: "CONFIRMED", EntityID: id,
			Summary:    "确认成本方案（中标）" + current.Header.ScenarioNo,
			BeforeJson: []byte(`{"status":"DRAFT"}`), AfterJson: afterJSON,
			OperatorID: op.ID, OperatorName: op.Name})
	})
	if e != nil {
		return CostScenarioView{}, e
	}
	return s.GetCostScenario(ctx, tenantID, id)
}

// SubmitCostToSales 将已确认的准确成本版本交还销售，之后销售才可据此生成客户报价。
func (s *Service) SubmitCostToSales(ctx context.Context, tenantID, id int64, op Operator) (CostScenarioView, error) {
	current, err := s.GetCostScenario(ctx, tenantID, id)
	if err != nil {
		return CostScenarioView{}, err
	}
	if current.Header.Status != "CONFIRMED" || current.Header.CustomerQuotationID != 0 {
		return CostScenarioView{}, apierr.Conflict("SC_COST_NOT_SUBMITTABLE", "只有尚未生成客户报价的已确认成本可以提交销售")
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		n, err := q.SubmitCostScenarioToSales(ctx, store.SubmitCostScenarioToSalesParams{
			OperatorID: &op.ID, OperatorName: op.Name, TenantID: tenantID, ID: id,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return apierr.Conflict("SC_COST_ALREADY_SUBMITTED", "该成本版本已经提交销售")
		}
		if err = q.MarkSourcingCaseSubmittedToSales(ctx, store.MarkSourcingCaseSubmittedToSalesParams{TenantID: tenantID, ID: current.Header.CaseID}); err != nil {
			return err
		}
		after, _ := json.Marshal(map[string]any{"scenarioNo": current.Header.ScenarioNo, "costVersion": current.Header.VersionNo, "requirementVersion": current.Header.RequirementVersionNo})
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{
			TenantID: tenantID, CaseID: current.Header.CaseID, Section: "HANDOFF", Action: "COST_SUBMITTED_TO_SALES",
			EntityID: id, Summary: "采购提交成本方案给销售", BeforeJson: []byte(`{"handoffStatus":"COST_CONFIRMED"}`),
			AfterJson: after, OperatorID: op.ID, OperatorName: op.Name,
		})
	})
	if err != nil {
		return CostScenarioView{}, err
	}
	return s.GetCostScenario(ctx, tenantID, id)
}

type CustomerQuotationDraft struct {
	Scenario store.GetCostScenarioRow
	Case     store.CostScenarioCaseRow
	Terms    store.CostScenarioTermsRow
	Lines    []store.ListCostScenarioLinesRow
}

// PrepareCustomerQuotation 从已确认的成本版本生成客户报价草稿；重复打开时保持幂等。
func (s *Service) PrepareCustomerQuotation(ctx context.Context, tenantID, id int64) (CustomerQuotationDraft, error) {
	v, e := s.GetCostScenario(ctx, tenantID, id)
	if e != nil {
		return CustomerQuotationDraft{}, e
	}
	if v.Header.Status != "CONFIRMED" {
		return CustomerQuotationDraft{}, apierr.Conflict("SC_COST_NOT_CONFIRMED", "请先确认成本方案")
	}
	if !v.Header.SubmittedToSalesAt.Valid {
		return CustomerQuotationDraft{}, apierr.Conflict("SC_COST_NOT_SUBMITTED", "采购尚未把该成本方案提交销售")
	}
	c, e := s.q.CostScenarioCase(ctx, store.CostScenarioCaseParams{TenantID: tenantID, ID: v.Header.CaseID})
	if e != nil {
		return CustomerQuotationDraft{}, e
	}
	t, e := s.q.CostScenarioTerms(ctx, store.CostScenarioTermsParams{TenantID: tenantID, CaseID: v.Header.CaseID})
	if e != nil {
		return CustomerQuotationDraft{}, e
	}
	return CustomerQuotationDraft{Scenario: v.Header, Case: c, Terms: t, Lines: v.Lines}, nil
}

// LinkCustomerQuotation 把已创建的客户报价关联回成本版本；成本状态与客户报价状态各自维护。
func (s *Service) LinkCustomerQuotation(ctx context.Context, tenantID, id, quotationID int64, quoteNo string) error {
	v, e := s.GetCostScenario(ctx, tenantID, id)
	if e != nil {
		return e
	}
	if v.Header.CustomerQuotationID != 0 {
		if v.Header.CustomerQuotationID == quotationID {
			return nil
		}
		return apierr.Conflict("SC_QUOTATION_EXISTS", "成本方案已经生成客户报价")
	}
	if !v.Header.SubmittedToSalesAt.Valid {
		return apierr.Conflict("SC_COST_NOT_SUBMITTED", "采购尚未把该成本方案提交销售")
	}
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		n, e := q.LinkCustomerQuotation(ctx, store.LinkCustomerQuotationParams{TenantID: tenantID, ID: id, CustomerQuotationID: &quotationID, CustomerQuoteNo: quoteNo})
		if e != nil {
			return e
		}
		if n != 1 {
			return apierr.Conflict("SC_QUOTATION_EXISTS", "成本方案已经生成客户报价")
		}
		return q.MarkSourcingCaseQuoted(ctx, store.MarkSourcingCaseQuotedParams{TenantID: tenantID, ID: v.Header.CaseID})
	})
}
