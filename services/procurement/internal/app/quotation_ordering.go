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
