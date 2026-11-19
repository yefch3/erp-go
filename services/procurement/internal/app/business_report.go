package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/shopspring/decimal"
)

func (s *Service) requireReportAccess(ctx context.Context) (int64, error) {
	op, ok := grpcx.OperatorFromContext(ctx)
	if !ok || op.TenantID <= 0 || op.EmployeeID <= 0 || s.permissions == nil {
		return 0, apierr.Permission("REPORT_PERMISSION", "没有查看经营报表的权限")
	}
	allowed, err := s.permissions.Allowed(ctx, op.EmployeeID, "report:business:read")
	if err != nil {
		return 0, err
	}
	if !allowed {
		return 0, apierr.Permission("REPORT_PERMISSION", "没有查看经营报表的权限")
	}
	return op.TenantID, nil
}

func (s *Service) ReportInquiries(ctx context.Context, req *prv1.GetReportInquiriesRequest) (*prv1.GetReportInquiriesResponse, error) {
	tenant, err := s.requireReportAccess(ctx)
	if err != nil {
		return nil, err
	}
	start, startErr := time.Parse(time.DateOnly, req.GetStartDate())
	cutoff, cutoffErr := time.Parse(time.DateOnly, req.GetCutoffDate())
	if startErr != nil || cutoffErr != nil || cutoff.Before(start) || cutoff.After(start.AddDate(0, 3, 0)) {
		return nil, apierr.Invalid("REPORT_PERIOD_INVALID", "报表日期无效")
	}
	rows, err := s.pool.Query(ctx, `
SELECT owner_id,max(owner_name),count(*)
FROM sourcing_cases
WHERE tenant_id=$1 AND deleted_at IS NULL AND status <> 'CANCELLED'
  AND (inquiry_submitted_at AT TIME ZONE 'Asia/Shanghai')::date BETWEEN $2::date AND $3::date
GROUP BY owner_id ORDER BY owner_id`, tenant, req.GetStartDate(), req.GetCutoffDate())
	if err != nil {
		return nil, err
	}
	employees, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*prv1.ReportInquiryCount, error) {
		count := &prv1.ReportInquiryCount{}
		err := row.Scan(&count.EmployeeId, &count.Employee, &count.Count)
		return count, err
	})
	return &prv1.GetReportInquiriesResponse{Employees: employees}, err
}

// Procurement and logistics payables share this ledger. Product costs are
// allocated by purchase-order line so an order covering multiple contracts
// contributes only this contract's share. Freight comes from its own payable.
const reportCostDocuments = `
WITH goods AS (
  SELECT p.id, sum(i.amount) AS amount,
         array_agg(r.contract_item_id ORDER BY i.id) AS item_ids,
         array_agg(i.qty::text ORDER BY i.id) AS quantities
  FROM purchase_requirements r
  JOIN purchase_order_items i ON i.tenant_id=r.tenant_id AND i.requirement_id=r.id
  JOIN purchase_orders p ON p.tenant_id=i.tenant_id AND p.id=i.po_id AND p.business_type='PROCUREMENT'
  WHERE r.tenant_id=$1 AND r.contract_id=$2
  GROUP BY p.id
)
SELECT coalesce(nullif(p.business_document_no,''),p.po_no), p.business_type, p.currency,
       coalesce(g.amount,p.total_amount)::text, p.transport_batch_no, g.item_ids, g.quantities,
       (p.signed_contract_key<>'' AND p.contract_verified_at IS NOT NULL AND
        (p.contract_verified_at AT TIME ZONE 'Asia/Shanghai')::date <= $4::date) AS verified,
       EXISTS (SELECT 1 FROM purchase_order_payment_closures cl
         WHERE cl.tenant_id=p.tenant_id AND cl.po_id=p.id AND cl.category='SETTLED' AND abs(cl.open_amount)<=0.01
           AND (cl.created_at AT TIME ZONE 'Asia/Shanghai')::date <= $4::date
           AND (cl.revoked_at IS NULL OR (cl.revoked_at AT TIME ZONE 'Asia/Shanghai')::date > $4::date)) AS settled
FROM purchase_orders p
LEFT JOIN goods g ON g.id=p.id
WHERE p.tenant_id=$1 AND p.status IN ('ORDERED','PARTIALLY_RECEIVED','RECEIVED')
  AND (coalesce(p.ordered_at,p.created_at) AT TIME ZONE 'Asia/Shanghai')::date <= $4::date
  AND (g.id IS NOT NULL OR (p.business_type='LOGISTICS' AND p.source_business_id>0 AND p.export_contract_no=$3))
ORDER BY p.business_type DESC,p.id
`

func (s *Service) ReportContractCosts(ctx context.Context, req *prv1.GetReportContractCostsRequest) (*prv1.GetReportContractCostsResponse, error) {
	tenant, err := s.requireReportAccess(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := time.Parse(time.DateOnly, req.GetCutoffDate()); err != nil || req.GetContractId() <= 0 || req.GetInternalContractNo() == "" {
		return nil, apierr.Invalid("REPORT_CONTRACT_INVALID", "无效的成本查询条件")
	}
	rows, err := s.pool.Query(ctx, reportCostDocuments, tenant, req.GetContractId(), req.GetInternalContractNo(), req.GetCutoffDate())
	if err != nil {
		return nil, err
	}
	out := &prv1.GetReportContractCostsResponse{Covered: len(req.GetItemQuantities()) > 0 && len(req.GetFreightBatches()) > 0}
	items := make(map[int64]decimal.Decimal)
	freight := make(map[int32]int)
	var doc prv1.ReportCostDocument
	var itemIDs []int64
	var quantities []string
	var batch int32
	var verified, settled bool
	_, err = pgx.ForEachRow(rows, []any{&doc.Number, &doc.Kind, &doc.Currency, &doc.Amount, &batch, &itemIDs, &quantities, &verified, &settled}, func() error {
		if !verified || !settled {
			out.Covered = false
		}
		if !verified {
			return nil
		}
		out.Costs = append(out.Costs, &prv1.ReportCostDocument{Number: doc.Number, Kind: doc.Kind, Currency: doc.Currency, Amount: doc.Amount})
		if doc.Kind == "LOGISTICS" {
			freight[batch]++
		}
		for i, id := range itemIDs {
			qty, err := decimal.NewFromString(quantities[i])
			if err != nil {
				return err
			}
			items[id] = items[id].Add(qty)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(items) != len(req.GetItemQuantities()) || len(freight) != len(req.GetFreightBatches()) {
		out.Covered = false
	}
	for id, raw := range req.GetItemQuantities() {
		qty, err := decimal.NewFromString(raw)
		if err != nil || !qty.IsPositive() || !items[id].Equal(qty) {
			out.Covered = false
		}
	}
	for _, batch := range req.GetFreightBatches() {
		if batch < 1 || freight[batch] != 1 {
			out.Covered = false
		}
	}
	return out, nil
}
