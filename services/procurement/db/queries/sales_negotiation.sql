-- name: SalesNegotiationCase :one
SELECT id,owner_id,owner_name,requirement_version_no,status,handoff_status
FROM sourcing_cases WHERE tenant_id=$1 AND id=$2;

-- name: SalesPlanSubmittedProcurement :one
SELECT id,target_sales_id,status FROM procurement_plans
WHERE tenant_id=$1 AND case_id=$2 AND id=$3 AND status='SUBMITTED_TO_SALES';

-- name: SalesPlanSubmittedShipping :one
SELECT sp.id,sp.target_sales_id,sp.status
FROM sourcing_shipping_plans sp
JOIN sourcing_shipping_requests sr ON sr.id=sp.request_id AND sr.tenant_id=sp.tenant_id
WHERE sp.tenant_id=$1 AND sr.case_id=$2 AND sp.id=$3 AND sp.status='SUBMITTED_TO_SALES';

-- name: SalesPlanProcurementCandidate :one
SELECT pi.id,pi.sourcing_line_id,pi.product_name,pi.available_qty::text,pi.uom_code
FROM procurement_plan_items pi
WHERE pi.tenant_id=$1 AND pi.plan_id=$2 AND pi.id=$3;

-- name: SalesPlanShippingCandidate :one
SELECT si.id,si.sourcing_line_id
FROM sourcing_shipping_plan_items si
WHERE si.tenant_id=$1 AND si.plan_id=$2 AND si.id=$3;

-- name: CountSalesPlanLines :one
SELECT count(*) FROM sourcing_lines
WHERE tenant_id=$1 AND case_id=$2 AND decision NOT IN ('SKIPPED','NO_MATCH');

-- name: SupersedePresentedSalesPlans :exec
UPDATE sourcing_sales_plans SET status='SUPERSEDED',updated_at=now()
WHERE tenant_id=$1 AND case_id=$2 AND status='PRESENTED';

-- name: CreateSalesPlan :one
INSERT INTO sourcing_sales_plans(tenant_id,case_id,plan_no,version_no,requirement_version_no,
 procurement_plan_id,shipping_plan_id,status,valid_until,customer_note,internal_note,created_by,created_by_name)
VALUES(sqlc.arg(tenant_id),sqlc.arg(case_id),
 'SP-'||to_char(current_date,'YYYYMMDD')||'-'||lpad(nextval('sourcing_sales_plan_no_seq')::text,6,'0'),
 (SELECT coalesce(max(version_no),0)+1 FROM sourcing_sales_plans WHERE tenant_id=sqlc.arg(tenant_id) AND case_id=sqlc.arg(case_id)),
 sqlc.arg(requirement_version_no),sqlc.arg(procurement_plan_id),nullif(sqlc.arg(shipping_plan_id)::bigint,0),
 'PRESENTED',sqlc.arg(valid_until)::text::date,sqlc.arg(customer_note),sqlc.arg(internal_note),
 sqlc.arg(created_by),sqlc.arg(created_by_name))
RETURNING id;

-- name: CreateSalesPlanItem :exec
INSERT INTO sourcing_sales_plan_items(tenant_id,plan_id,sourcing_line_id,procurement_plan_item_id,
 shipping_plan_item_id,option_type,priority,product_name,quoted_qty,uom_code,customer_currency,
 customer_unit_price,promised_delivery_date,line_note)
VALUES(sqlc.arg(tenant_id),sqlc.arg(plan_id),sqlc.arg(sourcing_line_id),sqlc.arg(procurement_plan_item_id),
 nullif(sqlc.arg(shipping_plan_item_id)::bigint,0),sqlc.arg(option_type),sqlc.arg(priority),
 sqlc.arg(product_name),sqlc.arg(quoted_qty)::text::numeric,sqlc.arg(uom_code),sqlc.arg(customer_currency),
 sqlc.arg(customer_unit_price)::text::numeric,nullif(sqlc.arg(promised_delivery_date)::text,'')::date,sqlc.arg(line_note));

-- name: ListSalesPlans :many
SELECT id,case_id,plan_no,version_no,requirement_version_no,procurement_plan_id,
 coalesce(shipping_plan_id,0)::bigint AS shipping_plan_id,status,valid_until::text,
 customer_note,internal_note,created_by,created_by_name,presented_at,created_at
FROM sourcing_sales_plans WHERE tenant_id=$1 AND case_id=$2 ORDER BY version_no DESC;

-- name: GetSalesPlan :one
SELECT id,case_id,plan_no,version_no,requirement_version_no,procurement_plan_id,
 coalesce(shipping_plan_id,0)::bigint AS shipping_plan_id,status,valid_until::text,
 customer_note,internal_note,created_by,created_by_name,presented_at,created_at
FROM sourcing_sales_plans WHERE tenant_id=$1 AND id=$2;

-- name: ListSalesPlanItems :many
SELECT id,sourcing_line_id,procurement_plan_item_id,coalesce(shipping_plan_item_id,0)::bigint AS shipping_plan_item_id,
 option_type,priority,product_name,quoted_qty::text,uom_code,customer_currency,customer_unit_price::text,
 coalesce(promised_delivery_date::text,'')::text AS promised_delivery_date,line_note
FROM sourcing_sales_plan_items WHERE tenant_id=$1 AND plan_id=$2
ORDER BY sourcing_line_id,option_type DESC,priority,id;

-- name: CreateCustomerFeedback :one
INSERT INTO sourcing_customer_feedback(tenant_id,case_id,sales_plan_id,contact_name,channel,result,summary,
 contacted_at,created_by,created_by_name)
VALUES(sqlc.arg(tenant_id),sqlc.arg(case_id),sqlc.arg(sales_plan_id),sqlc.arg(contact_name),sqlc.arg(channel),
 sqlc.arg(result),sqlc.arg(summary),sqlc.arg(contacted_at)::text::timestamptz,sqlc.arg(created_by),sqlc.arg(created_by_name))
RETURNING id;

-- name: ListCustomerFeedback :many
SELECT id,case_id,sales_plan_id,contact_name,channel,result,summary,contacted_at,created_by,created_by_name,created_at
FROM sourcing_customer_feedback WHERE tenant_id=$1 AND case_id=$2 ORDER BY contacted_at DESC,id DESC;

-- name: MarkSalesPlanReworkRequested :exec
UPDATE sourcing_sales_plans SET status='REWORK_REQUESTED',updated_at=now()
WHERE tenant_id=$1 AND id=$2 AND status='PRESENTED';

-- name: ShippingReworkQuote :one
SELECT ol.id AS option_line_id,ol.sourcing_line_id,o.created_by AS shipping_id,o.created_by_name AS shipping_name,
 o.carrier_forwarder,sl.product AS product_name
FROM sourcing_shipping_option_lines ol
JOIN sourcing_shipping_options o ON o.id=ol.option_id AND o.tenant_id=ol.tenant_id
JOIN sourcing_shipping_requests sr ON sr.id=o.request_id AND sr.tenant_id=o.tenant_id
JOIN sourcing_lines sl ON sl.id=ol.sourcing_line_id AND sl.tenant_id=ol.tenant_id
WHERE ol.tenant_id=$1 AND sr.case_id=$2 AND ol.id=$3;

-- name: ShippingReworkProduct :one
SELECT id,product FROM sourcing_lines WHERE tenant_id=$1 AND case_id=$2 AND id=$3;

-- name: CreateShippingRework :one
INSERT INTO sourcing_shipping_rework_requests(tenant_id,case_id,sales_plan_id,sourcing_line_id,
 shipping_option_line_id,request_type,scope_type,assigned_shipping_id,assigned_shipping_name,
 carrier_forwarder,product_name,reason,created_by,created_by_name)
VALUES(sqlc.arg(tenant_id),sqlc.arg(case_id),nullif(sqlc.arg(sales_plan_id)::bigint,0),
 nullif(sqlc.arg(sourcing_line_id)::bigint,0),nullif(sqlc.arg(shipping_option_line_id)::bigint,0),
 sqlc.arg(request_type),sqlc.arg(scope_type),nullif(sqlc.arg(assigned_shipping_id)::bigint,0),
 sqlc.arg(assigned_shipping_name),sqlc.arg(carrier_forwarder),sqlc.arg(product_name),sqlc.arg(reason),
 sqlc.arg(created_by),sqlc.arg(created_by_name)) RETURNING id;

-- name: ListShippingReworks :many
SELECT id,case_id,coalesce(sales_plan_id,0)::bigint AS sales_plan_id,
 coalesce(sourcing_line_id,0)::bigint AS sourcing_line_id,
 coalesce(shipping_option_line_id,0)::bigint AS shipping_option_line_id,request_type,scope_type,
 coalesce(assigned_shipping_id,0)::bigint AS assigned_shipping_id,assigned_shipping_name,
 carrier_forwarder,product_name,reason,status,created_by,created_by_name,created_at,
 coalesce(resolved_by,0)::bigint AS resolved_by,resolved_by_name,resolved_at,resolution_note
FROM sourcing_shipping_rework_requests WHERE tenant_id=$1 AND case_id=$2 ORDER BY created_at DESC;

-- name: ListMyShippingReworks :many
SELECT rr.id,rr.case_id,coalesce(rr.sales_plan_id,0)::bigint AS sales_plan_id,
 coalesce(rr.sourcing_line_id,0)::bigint AS sourcing_line_id,
 coalesce(rr.shipping_option_line_id,0)::bigint AS shipping_option_line_id,rr.request_type,rr.scope_type,
 coalesce(rr.assigned_shipping_id,0)::bigint AS assigned_shipping_id,rr.assigned_shipping_name,
 rr.carrier_forwarder,rr.product_name,rr.reason,rr.status,rr.created_by,rr.created_by_name,rr.created_at,
 coalesce(rr.resolved_by,0)::bigint AS resolved_by,rr.resolved_by_name,rr.resolved_at,rr.resolution_note,
 sc.case_no,sc.title AS case_title
FROM sourcing_shipping_rework_requests rr
JOIN sourcing_cases sc ON sc.tenant_id=rr.tenant_id AND sc.id=rr.case_id
WHERE rr.tenant_id=sqlc.arg(tenant_id) AND rr.status='OPEN'
  AND (rr.assigned_shipping_id=sqlc.arg(employee_id) OR rr.assigned_shipping_id IS NULL)
ORDER BY rr.created_at DESC;

-- name: GetShippingRework :one
SELECT id,case_id,coalesce(sales_plan_id,0)::bigint AS sales_plan_id,
 coalesce(sourcing_line_id,0)::bigint AS sourcing_line_id,
 coalesce(shipping_option_line_id,0)::bigint AS shipping_option_line_id,request_type,scope_type,
 coalesce(assigned_shipping_id,0)::bigint AS assigned_shipping_id,assigned_shipping_name,
 carrier_forwarder,product_name,reason,status,created_by,created_by_name,created_at,
 coalesce(resolved_by,0)::bigint AS resolved_by,resolved_by_name,resolved_at,resolution_note
FROM sourcing_shipping_rework_requests WHERE tenant_id=$1 AND id=$2;

-- name: ResolveShippingRework :execrows
UPDATE sourcing_shipping_rework_requests SET status='RESOLVED',resolved_by=sqlc.arg(operator_id),
 resolved_by_name=sqlc.arg(operator_name),resolved_at=now(),resolution_note=sqlc.arg(resolution_note)
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND status='OPEN';
