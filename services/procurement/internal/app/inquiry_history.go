package app

import (
	"context"
	"encoding/json"
)

// Historical plans are projections, never imported into inquiry_quotes. Only
// an explicit sales-submission timestamp is evidence of sales visibility.
// Prices come from frozen plan items, not mutable supplier quote rows.
func (s *Service) inquiryHistory(ctx context.Context, tenant, id int64, view string) ([]InquiryQuoteView, error) {
	out := []InquiryQuoteView{}
	queries := []struct{ kind, sql string }{
		{"PROCUREMENT", `SELECT jsonb_build_object(
 'id','history-procurement-'||p.id::text||'-'||min(i.id)::text,
 'kind','PROCUREMENT','historical',true,'authorId',i.buyer_id::text,'author',i.buyer_name,
 'submittedAt',p.submitted_to_sales_at::text,'updatedBy',p.created_by_name,'updatedAt',p.updated_at::text,
 'body',jsonb_build_object('company',coalesce(nullif(i.factory_name,''),i.supplier_name),'currency',trim(i.currency),
 'paymentTerms',i.payment_terms,'incoterm',i.incoterm,'validUntil',coalesce(i.valid_until::text,''),
 'remark',p.manager_note,'attachments','[]'::jsonb,
 'prices',jsonb_agg(jsonb_build_object('productId',i.sourcing_line_id::text,'price',i.unit_price::text,
 'delivery',CASE WHEN i.lead_time IS NULL THEN '' ELSE i.lead_time::text||' 天' END,
 'remark',concat_ws('；',nullif(i.reason,''),nullif(i.risk,''))) ORDER BY i.id)))
 FROM procurement_plans p JOIN procurement_plan_items i ON i.tenant_id=p.tenant_id AND i.plan_id=p.id
 WHERE p.tenant_id=$1 AND p.case_id=$2 AND p.submitted_to_sales_at IS NOT NULL
 GROUP BY p.id,i.factory_name,i.supplier_name,i.currency,i.payment_terms,i.incoterm,i.valid_until,i.buyer_id,i.buyer_name ORDER BY p.id,min(i.id)`},
		{"LOGISTICS", `SELECT jsonb_build_object(
 'id','history-logistics-'||p.id::text||'-'||min(i.id)::text,
 'kind','LOGISTICS','historical',true,'authorId',i.shipping_employee_id::text,'author',i.shipping_employee_name,
 'submittedAt',p.submitted_to_sales_at::text,'updatedBy',p.created_by_name,'updatedAt',p.updated_at::text,
 'body',jsonb_build_object('company',i.carrier_forwarder,'route',i.service_option_name,
 'loadingPort',i.port_of_loading,'destinationPort',i.port_of_discharge,
 'departure',coalesce(i.estimated_departure::text,''),'arrival',coalesce(i.estimated_arrival::text,''),
 'validUntil',coalesce(i.valid_until::text,''),'remark',p.manager_note,'attachments','[]'::jsonb,
 'cargoIds',jsonb_agg(i.sourcing_line_id::text ORDER BY i.id),
 'charges',jsonb_agg(jsonb_build_object('name',i.product_name||'（历史运费）','amount',i.total_freight::text,
 'currency',trim(i.currency),'quantity','1','unit','原方案费用','subtotal',i.total_freight::text,
 'remark',concat_ws('；',i.charge_basis||' 单价 '||i.unit_rate::text,nullif(i.reason,''),nullif(i.risk,''))) ORDER BY i.id)))
 FROM sourcing_shipping_plans p JOIN sourcing_shipping_requests r ON r.tenant_id=p.tenant_id AND r.id=p.request_id
 JOIN sourcing_shipping_plan_items i ON i.tenant_id=p.tenant_id AND i.plan_id=p.id
 WHERE p.tenant_id=$1 AND r.case_id=$2 AND p.submitted_to_sales_at IS NOT NULL
 GROUP BY p.id,i.carrier_forwarder,i.service_option_name,i.port_of_loading,i.port_of_discharge,
 i.estimated_departure,i.estimated_arrival,i.valid_until,i.shipping_employee_id,i.shipping_employee_name ORDER BY p.id,min(i.id)`},
	}
	for _, query := range queries {
		if (view == "PROCUREMENT" || view == "LOGISTICS") && view != query.kind {
			continue
		}
		rows, err := s.pool.Query(ctx, query.sql, tenant, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var raw []byte
			var q InquiryQuoteView
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return nil, err
			}
			if err = json.Unmarshal(raw, &q); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, q)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
