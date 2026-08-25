package app

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

// QuotationAccepted 是出口服务在客户接受报价时发布的采购交接快照。
// 采购服务只依赖稳定的报价、客户和成本方案标识，不回调出口服务补数据。
type QuotationAccepted struct {
	QuotationID    int64  `json:"quotation_id"`
	QuotationNo    string `json:"quotation_no"`
	CustomerID     int64  `json:"customer_id"`
	CustomerName   string `json:"customer_name"`
	CostScenarioID int64  `json:"cost_scenario_id"`
	SourcingCaseID int64  `json:"sourcing_case_id"`
}

// QuotationRejected 是客户拒绝报价时传回采购的不可变来源标识。
// 字段与接受事件保持一致，便于采购准确定位原成本版本和询价项目。
type QuotationRejected QuotationAccepted

// ReturnRejectedQuotationToCosting 只让被客户明确拒绝的成本版本失效，并将原询价项目退回成本测算。
// 重复消费同一个事件不会影响其他版本，也不会把等待中或已接受的报价误判为失效。
func (s *Service) ReturnRejectedQuotationToCosting(
	ctx context.Context,
	tenantID int64,
	e QuotationRejected,
	log *slog.Logger,
) error {
	if e.QuotationID == 0 || e.CostScenarioID == 0 || e.SourcingCaseID == 0 {
		log.Warn("rejected quotation has incomplete sourcing trace; cost scenario unchanged",
			"quotation_id", e.QuotationID, "scenario_id", e.CostScenarioID, "case_id", e.SourcingCaseID)
		return nil
	}
	changed := false
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		hasAccepted, err := q.HasAcceptedQuotationRequirements(ctx, store.HasAcceptedQuotationRequirementsParams{
			TenantID: tenantID, CaseID: e.SourcingCaseID,
		})
		if err != nil {
			return err
		}
		n, err := q.SupersedeRejectedQuotationScenario(ctx, store.SupersedeRejectedQuotationScenarioParams{
			TenantID: tenantID, ID: e.CostScenarioID, QuotationID: &e.QuotationID,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		changed = true
		if hasAccepted {
			return nil
		}
		_, err = q.ReturnRejectedQuotationCaseToCosting(ctx, store.ReturnRejectedQuotationCaseToCostingParams{
			TenantID: tenantID, ID: e.SourcingCaseID,
		})
		return err
	})
	if err != nil {
		return err
	}
	if changed {
		s.nudge(ctx, tenantID)
		log.Info("rejected quotation cost scenario superseded",
			"quotation_id", e.QuotationID, "scenario_id", e.CostScenarioID, "case_id", e.SourcingCaseID)
	}
	return nil
}

// RequirementsFromAcceptedQuotation 把已接受报价按确认成本方案转成待下单明细。
// 每条明细锁定当时选中的供应商报价；工厂允许为空，内部产品也允许为 0。
func (s *Service) RequirementsFromAcceptedQuotation(
	ctx context.Context,
	tenantID int64,
	e QuotationAccepted,
	log *slog.Logger,
) error {
	if e.CostScenarioID == 0 {
		log.Warn("accepted quotation has no confirmed cost scenario; no ordering task created",
			"quotation_id", e.QuotationID, "quotation_no", e.QuotationNo)
		return nil
	}
	scenario, err := s.q.AcceptedQuotationScenario(ctx, store.AcceptedQuotationScenarioParams{
		TenantID: tenantID, QuotationID: &e.QuotationID,
	})
	if err == pgx.ErrNoRows {
		log.Warn("accepted quotation cost scenario not found or not confirmed",
			"quotation_id", e.QuotationID, "cost_scenario_id", e.CostScenarioID)
		return nil
	}
	if err != nil {
		return err
	}
	lines, err := s.q.AcceptedQuotationLines(ctx, store.AcceptedQuotationLinesParams{
		TenantID: tenantID, ScenarioID: scenario.ID,
	})
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		log.Warn("accepted quotation confirmed scenario contains no ordering lines",
			"quotation_id", e.QuotationID, "scenario_id", scenario.ID)
		return nil
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		for _, line := range lines {
			if _, err := q.UpsertQuotationRequirement(ctx, store.UpsertQuotationRequirementParams{
				TenantID: tenantID, CustomerName: e.CustomerName,
				ProductID: line.ProductID, SkuID: line.SkuID,
				ProductName: line.ProductName, Spec: line.SpecSnapshot,
				UomCode: line.UomCode, RequiredQty: line.Qty,
				QuotationID: e.QuotationID, QuotationNo: e.QuotationNo,
				CostScenarioID: scenario.ID, CostScenarioNo: scenario.ScenarioNo,
				SourcingCaseID: scenario.CaseID, SourcingLineID: line.SourcingLineID,
				SupplierQuoteLineID: line.SupplierQuoteLineID,
				SupplierID:          line.SupplierID, SupplierCode: line.SupplierCode, SupplierName: line.SupplierName,
				FactoryID: line.FactoryID, FactoryCode: line.FactoryCode, FactoryName: line.FactoryName,
				SourceCurrency: line.SourceCurrency, SourceUnitPrice: line.SourceUnitPrice,
				Moq: line.Moq, LeadTime: line.LeadTime,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.nudge(ctx, tenantID)
	log.Info("ordering requirements created from accepted quotation",
		"quotation_id", e.QuotationID, "quotation_no", e.QuotationNo,
		"scenario_id", scenario.ID, "lines", len(lines))
	return nil
}
