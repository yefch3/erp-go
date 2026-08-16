package app

import (
	"context"
	"fmt"
)

type DeactivationImpactItem struct {
	Code  string
	Label string
	Count int64
}

// countReferencedRows 只执行代码内固定的查询；表不存在时返回 0，便于独立服务测试数据库运行。
func (s *Service) countReferencedRows(ctx context.Context, table, query string, tenantID, id int64) (int64, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil || !exists {
		return 0, err
	}
	var count int64
	if err := s.pool.QueryRow(ctx, query, tenantID, id).Scan(&count); err != nil {
		return 0, fmt.Errorf("count %s references: %w", table, err)
	}
	return count, nil
}

// DeactivationImpact 汇总停用主数据前仍会受到影响的业务记录；历史记录只计数，不删除也不改写。
func (s *Service) DeactivationImpact(ctx context.Context, tenantID int64, entityType string, id int64) ([]DeactivationImpactItem, error) {
	type spec struct{ table, code, label, query string }
	var specs []spec
	switch entityType {
	case "CUSTOMER":
		specs = []spec{
			{"shipping_schedules", "SHIPPING_SCHEDULE", "进行中的船期", `SELECT count(*) FROM shipping_schedules WHERE tenant_id=$1 AND customer_id=$2 AND status NOT IN ('ARRIVED','COMPLETED','CANCELLED')`},
			{"quotations", "QUOTATION", "未结束的报价", `SELECT count(*) FROM quotations WHERE tenant_id=$1 AND customer_id=$2 AND status NOT IN ('ACCEPTED','REJECTED','EXPIRED','CANCELLED')`},
			{"contracts", "CONTRACT", "未结束的合同", `SELECT count(*) FROM contracts WHERE tenant_id=$1 AND customer_id=$2 AND status NOT IN ('COMPLETED','CANCELLED')`},
			{"sourcing_cases", "SOURCING_CASE", "进行中的寻源", `SELECT count(*) FROM sourcing_cases WHERE tenant_id=$1 AND customer_id=$2 AND status NOT IN ('CUSTOMER_QUOTE_CREATED','CANCELLED')`},
		}
	case "SUPPLIER":
		specs = []spec{
			{"purchase_orders", "PURCHASE_ORDER", "未结束的采购单", `SELECT count(*) FROM purchase_orders WHERE tenant_id=$1 AND supplier_id=$2 AND status NOT IN ('RECEIVED','CANCELLED')`},
			{"factory_rfqs", "FACTORY_RFQ", "未结束的询价", `SELECT count(*) FROM factory_rfqs WHERE tenant_id=$1 AND supplier_id=$2 AND status NOT IN ('CLOSED','CANCELLED')`},
			{"factories", "AVAILABLE_FACTORY", "将联动暂停的工厂", `SELECT count(*) FROM factories WHERE tenant_id=$1 AND supplier_id=$2 AND status IN ('PREPARING','COOPERATING')`},
		}
	case "FACTORY":
		specs = []spec{
			{"factory_contacts", "FACTORY_CONTACT", "有效联系人", `SELECT count(*) FROM factory_contacts WHERE tenant_id=$1 AND factory_id=$2 AND status='ACTIVE'`},
			{"factory_owners", "FACTORY_OWNER", "有效负责人", `SELECT count(*) FROM factory_owners WHERE tenant_id=$1 AND factory_id=$2 AND status='ACTIVE'`},
			{"factory_capabilities", "FACTORY_CAPABILITY", "产能资料", `SELECT count(*) FROM factory_capabilities WHERE tenant_id=$1 AND factory_id=$2`},
		}
	case "PORT":
		specs = []spec{
			{"shipping_schedules", "SHIPPING_SCHEDULE", "进行中的相关船期", `SELECT count(*) FROM shipping_schedules WHERE tenant_id=$1 AND (loading_port_id=$2 OR discharge_port_id=$2) AND status NOT IN ('ARRIVED','COMPLETED','CANCELLED')`},
			{"shipping_route_nodes", "ROUTE_NODE", "未完成的港口节点", `SELECT count(*) FROM shipping_route_nodes WHERE tenant_id=$1 AND port_id=$2 AND is_active=true AND node_status NOT IN ('DEPARTED','SKIPPED')`},
		}
	default:
		return nil, fmt.Errorf("unsupported master data entity type %q", entityType)
	}
	items := make([]DeactivationImpactItem, 0, len(specs))
	for _, item := range specs {
		count, err := s.countReferencedRows(ctx, item.table, item.query, tenantID, id)
		if err != nil {
			return nil, err
		}
		if count > 0 {
			items = append(items, DeactivationImpactItem{Code: item.code, Label: item.label, Count: count})
		}
	}
	return items, nil
}
