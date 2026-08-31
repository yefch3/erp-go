-- name: CustomerSelectionCandidate :one
SELECT spi.id AS sales_plan_item_id,spi.sourcing_line_id,spi.procurement_plan_item_id,
 spi.shipping_plan_item_id,spi.product_name,spi.quoted_qty::text,spi.uom_code,
 spi.customer_currency,spi.customer_unit_price::text,
 coalesce(spi.promised_delivery_date::text,'')::text AS promised_delivery_date,spi.line_note,
 ppi.supplier_quote_line_id,ppi.buyer_id,ppi.buyer_name,ppi.supplier_id,ppi.supplier_name,
 coalesce(sspi.shipping_option_line_id,0)::bigint AS shipping_option_line_id,
 coalesce(sspi.shipping_employee_id,0)::bigint AS shipping_employee_id,
 coalesce(sspi.shipping_employee_name,'')::text AS shipping_employee_name,
 coalesce(sspi.carrier_forwarder,'')::text AS carrier_forwarder
FROM sourcing_sales_plan_items spi
JOIN sourcing_sales_plans sp ON sp.id=spi.plan_id AND sp.tenant_id=spi.tenant_id
JOIN procurement_plan_items ppi ON ppi.id=spi.procurement_plan_item_id AND ppi.tenant_id=spi.tenant_id
LEFT JOIN sourcing_shipping_plan_items sspi ON sspi.id=spi.shipping_plan_item_id AND sspi.tenant_id=spi.tenant_id
WHERE spi.tenant_id=$1 AND sp.case_id=$2 AND sp.id=$3 AND spi.id=$4
  AND sp.status='PRESENTED' AND sp.valid_until>=current_date;

-- name: InvalidateActiveCustomerSelections :exec
UPDATE sourcing_customer_selections
SET status='INVALIDATED',invalidated_at=now(),invalidated_reason=sqlc.arg(reason),updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND case_id=sqlc.arg(case_id)
  AND status IN ('FINAL_RECHECK_PENDING','FINAL_RECHECKED');

-- name: InvalidateExpiredCustomerSelections :exec
UPDATE sourcing_customer_selections s
SET status='INVALIDATED',invalidated_at=now(),
    invalidated_reason='客户沟通方案已超过有效期',updated_at=now()
FROM sourcing_sales_plans sp
WHERE s.tenant_id=sqlc.arg(tenant_id) AND s.case_id=sqlc.arg(case_id)
  AND sp.tenant_id=s.tenant_id AND sp.id=s.sales_plan_id
  AND s.status IN ('FINAL_RECHECK_PENDING','FINAL_RECHECKED')
  AND sp.valid_until<current_date;

-- name: CreateCustomerSelection :one
INSERT INTO sourcing_customer_selections(tenant_id,case_id,sales_plan_id,selection_no,version_no,
 requirement_version_no,status,customer_contact,confirmation_note,customer_confirmed_at,created_by,created_by_name)
VALUES(sqlc.arg(tenant_id),sqlc.arg(case_id),sqlc.arg(sales_plan_id),
 'CS-'||to_char(current_date,'YYYYMMDD')||'-'||lpad(nextval('sourcing_customer_selection_no_seq')::text,6,'0'),
 (SELECT coalesce(max(version_no),0)+1 FROM sourcing_customer_selections
  WHERE tenant_id=sqlc.arg(tenant_id) AND case_id=sqlc.arg(case_id)),
 sqlc.arg(requirement_version_no),'FINAL_RECHECK_PENDING',sqlc.arg(customer_contact),
 sqlc.arg(confirmation_note),sqlc.arg(customer_confirmed_at)::text::timestamptz,
 sqlc.arg(created_by),sqlc.arg(created_by_name))
RETURNING id;

-- name: CreateCustomerSelectionItem :one
INSERT INTO sourcing_customer_selection_items(tenant_id,selection_id,sales_plan_item_id,sourcing_line_id,
 procurement_plan_item_id,supplier_quote_line_id,shipping_plan_item_id,shipping_option_line_id,
 product_name,confirmed_qty,uom_code,customer_currency,customer_unit_price,promised_delivery_date,line_note)
VALUES(sqlc.arg(tenant_id),sqlc.arg(selection_id),sqlc.arg(sales_plan_item_id),sqlc.arg(sourcing_line_id),
 sqlc.arg(procurement_plan_item_id),sqlc.arg(supplier_quote_line_id),
 nullif(sqlc.arg(shipping_plan_item_id)::bigint,0),nullif(sqlc.arg(shipping_option_line_id)::bigint,0),
 sqlc.arg(product_name),sqlc.arg(confirmed_qty)::text::numeric,sqlc.arg(uom_code),
 sqlc.arg(customer_currency),sqlc.arg(customer_unit_price)::text::numeric,
 nullif(sqlc.arg(promised_delivery_date)::text,'')::date,sqlc.arg(line_note))
RETURNING id;

-- name: CreateFinalProcurementRecheckTask :exec
INSERT INTO sourcing_final_recheck_tasks(tenant_id,selection_id,selection_item_id,task_domain,procurement_rework_id)
VALUES($1,$2,$3,'PROCUREMENT',$4);

-- name: CreateFinalShippingRecheckTask :exec
INSERT INTO sourcing_final_recheck_tasks(tenant_id,selection_id,selection_item_id,task_domain,shipping_rework_id)
VALUES($1,$2,$3,'SHIPPING',$4);

-- name: ListCustomerSelections :many
SELECT id,case_id,sales_plan_id,selection_no,version_no,requirement_version_no,status,
 customer_contact,confirmation_note,customer_confirmed_at,created_by,created_by_name,created_at,
 coalesce(final_rechecked_at::text,'')::text AS final_rechecked_at,
 coalesce(invalidated_at::text,'')::text AS invalidated_at,invalidated_reason
FROM sourcing_customer_selections WHERE tenant_id=$1 AND case_id=$2 ORDER BY version_no DESC;

-- name: ListCustomerSelectionItems :many
SELECT id,sales_plan_item_id,sourcing_line_id,procurement_plan_item_id,supplier_quote_line_id,
 coalesce(shipping_plan_item_id,0)::bigint AS shipping_plan_item_id,
 coalesce(shipping_option_line_id,0)::bigint AS shipping_option_line_id,
 product_name,confirmed_qty::text,uom_code,customer_currency,customer_unit_price::text,
 coalesce(promised_delivery_date::text,'')::text AS promised_delivery_date,line_note
FROM sourcing_customer_selection_items WHERE tenant_id=$1 AND selection_id=$2 ORDER BY id;

-- name: ListFinalRecheckTasks :many
SELECT id,selection_item_id,task_domain,coalesce(procurement_rework_id,0)::bigint AS procurement_rework_id,
 coalesce(shipping_rework_id,0)::bigint AS shipping_rework_id,status,
 coalesce(resolved_at::text,'')::text AS resolved_at
FROM sourcing_final_recheck_tasks WHERE tenant_id=$1 AND selection_id=$2 ORDER BY selection_item_id,task_domain;

-- name: ResolveFinalTaskByProcurementRework :exec
UPDATE sourcing_final_recheck_tasks SET status='RESOLVED',resolved_at=now()
WHERE tenant_id=$1 AND procurement_rework_id=$2 AND status='OPEN';

-- name: ResolveFinalTaskByShippingRework :exec
UPDATE sourcing_final_recheck_tasks SET status='RESOLVED',resolved_at=now()
WHERE tenant_id=$1 AND shipping_rework_id=$2 AND status='OPEN';

-- name: CompleteReadyCustomerSelections :exec
UPDATE sourcing_customer_selections s
SET status='FINAL_RECHECKED',final_rechecked_at=now(),updated_at=now()
WHERE s.tenant_id=$1 AND s.status='FINAL_RECHECK_PENDING'
  AND NOT EXISTS (SELECT 1 FROM sourcing_final_recheck_tasks t
                  WHERE t.tenant_id=s.tenant_id AND t.selection_id=s.id AND t.status='OPEN');
