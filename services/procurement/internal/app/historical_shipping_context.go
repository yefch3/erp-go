package app

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/sgao19/erp-go/pkg/apierr"
)

func normalizeHistoricalShippingContext(contractNo, port string) (string, string, error) {
	contractNo, port = strings.TrimSpace(contractNo), strings.TrimSpace(port)
	if utf8.RuneCountInString(contractNo) > 50 || utf8.RuneCountInString(port) > 200 {
		return "", "", apierr.Invalid("PO_HISTORY_CONTEXT_TOO_LONG", "外贸合同号最多 50 字，目的港最多 200 字")
	}
	return contractNo, port, nil
}

// Metadata only, including orders already received/paid: never update prices,
// quantities, financial contract snapshots, source IDs or workflow status.
func (s *Service) UpdateHistoricalShippingContext(ctx context.Context, tenantID, orderID int64, contractNo, port string, op Operator) error {
	if tenantID <= 0 || op.ID <= 0 {
		return apierr.Permission("PO_OPERATOR_REQUIRED", "请重新登录")
	}
	if err := s.AuthorizeOrder(ctx, tenantID, orderID, op); err != nil {
		return err
	}
	contractNo, port, err := normalizeHistoricalShippingContext(contractNo, port)
	if err != nil {
		return err
	}
	result, err := s.pool.Exec(ctx, `UPDATE historical_purchase_orders SET export_contract_no=$3,destination_port=$4 WHERE tenant_id=$1 AND po_id=$2`, tenantID, orderID, contractNo, port)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return apierr.NotFound("PO_HISTORY_NOT_FOUND", "历史补录采购单不存在")
	}
	s.nudge(ctx, tenantID)
	return nil
}

// Not an Order projection: no prices, amounts, quantities, payment information
// or order IDs. The trusted gateway derives contractNo from a readable handoff.
type ShippingHistoricalPurchase struct {
	PONo, SupplierName, ExportContractNo, DestinationPort, Remark string
}

func (s *Service) ListShippingHistoricalPurchases(ctx context.Context, tenantID int64, contractNo string, op Operator) ([]ShippingHistoricalPurchase, error) {
	access, ok := s.scopes.(InquiryAccess)
	if !ok || tenantID <= 0 || op.ID <= 0 {
		return nil, apierr.Permission("SHIPPING_HISTORY_PERMISSION", "没有查看海运历史采购信息的权限")
	}
	allowed, err := access.HasPermission(ctx, op.ID, "shipping:schedule:read")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, apierr.Permission("SHIPPING_HISTORY_PERMISSION", "没有查看海运历史采购信息的权限")
	}
	contractNo = strings.TrimSpace(contractNo)
	if contractNo == "" {
		return []ShippingHistoricalPurchase{}, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT o.po_no,o.supplier_name,h.export_contract_no,h.destination_port,o.remark
 FROM historical_purchase_orders h JOIN purchase_orders o ON o.tenant_id=h.tenant_id AND o.id=h.po_id
 WHERE h.tenant_id=$1 AND h.export_contract_no=$2 AND o.business_type='PROCUREMENT'
 AND o.source_business_id=0 AND o.status IN ('ORDERED','PARTIALLY_RECEIVED','RECEIVED') ORDER BY o.id`, tenantID, contractNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShippingHistoricalPurchase{}
	for rows.Next() {
		var row ShippingHistoricalPurchase
		if err = rows.Scan(&row.PONo, &row.SupplierName, &row.ExportContractNo, &row.DestinationPort, &row.Remark); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
