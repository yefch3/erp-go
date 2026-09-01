-- name: CustomerSelectionCandidate :one
SELECT spi.id AS sales_plan_item_id,spi.sourcing_line_id,spi.procurement_plan_item_id,
 spi.product_name,spi.quoted_qty::text,spi.uom_code,
 spi.customer_currency,spi.customer_unit_price::text,
 coalesce(spi.promised_delivery_date::text,'')::text AS promised_delivery_date,spi.line_note,
 ppi.supplier_quote_line_id,ppi.buyer_id,ppi.buyer_name,ppi.supplier_id,ppi.supplier_name,
 coalesce(ppi.factory_id,0)::bigint AS factory_id,ppi.factory_name,sl.port AS required_destination_port
FROM sourcing_sales_plan_items spi
JOIN sourcing_sales_plans sp ON sp.id=spi.plan_id AND sp.tenant_id=spi.tenant_id
JOIN procurement_plan_items ppi ON ppi.id=spi.procurement_plan_item_id AND ppi.tenant_id=spi.tenant_id
JOIN sourcing_lines sl ON sl.id=spi.sourcing_line_id AND sl.tenant_id=spi.tenant_id
WHERE spi.tenant_id=$1 AND sp.case_id=$2 AND sp.id=$3 AND spi.id=$4
  AND sp.status='PRESENTED' AND sp.valid_until>=current_date;

-- name: CustomerShipmentCandidate :one
SELECT sso.id,sso.shipping_option_id,sso.carrier_forwarder,sso.service_option_name,
 sso.shipping_employee_id,sso.shipping_employee_name,sso.customer_currency,
 sso.customer_freight_amount::text,sso.charge_basis,sso.port_of_loading,sso.port_of_discharge,
 coalesce(sso.estimated_departure::text,'')::text AS estimated_departure,
 coalesce(sso.estimated_arrival::text,'')::text AS estimated_arrival,
 coalesce(sso.valid_until::text,'')::text AS valid_until,sso.customer_note
FROM sourcing_sales_shipping_options sso
JOIN sourcing_sales_plans sp ON sp.tenant_id=sso.tenant_id AND sp.id=sso.plan_id
WHERE sso.tenant_id=$1 AND sp.case_id=$2 AND sp.id=$3 AND sso.id=$4
  AND sp.status='PRESENTED' AND sp.valid_until>=current_date
  AND (sso.valid_until IS NULL OR sso.valid_until>=current_date);

-- name: CustomerShipmentCandidateLines :many
SELECT sourcing_line_id,shipping_plan_item_id,shipping_option_line_id,product_name,
 quoted_qty::text,uom_code
FROM sourcing_sales_shipping_option_lines
WHERE tenant_id=$1 AND sales_shipping_option_id=$2 ORDER BY sourcing_line_id;

-- name: InvalidateActiveCustomerSelections :exec
UPDATE sourcing_customer_selections
SET status='INVALIDATED',invalidated_at=now(),invalidated_reason=sqlc.arg(reason),updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND case_id=sqlc.arg(case_id)
  AND status IN ('INTENT_RECHECK_PENDING','AWAITING_CUSTOMER_CONFIRMATION','CUSTOMER_CONFIRMED');

-- name: InvalidateExpiredCustomerSelections :exec
UPDATE sourcing_customer_selections s
SET status='INVALIDATED',invalidated_at=now(),
    invalidated_reason='客户沟通方案已超过有效期',updated_at=now()
FROM sourcing_sales_plans sp
WHERE s.tenant_id=sqlc.arg(tenant_id) AND s.case_id=sqlc.arg(case_id)
  AND sp.tenant_id=s.tenant_id AND sp.id=s.sales_plan_id
  AND s.status IN ('INTENT_RECHECK_PENDING','AWAITING_CUSTOMER_CONFIRMATION','CUSTOMER_CONFIRMED')
  AND sp.valid_until<current_date;

-- name: CreateCustomerSelection :one
INSERT INTO sourcing_customer_selections(tenant_id,case_id,sales_plan_id,selection_no,version_no,
 requirement_version_no,status,customer_contact,confirmation_note,customer_confirmed_at,created_by,created_by_name)
VALUES(sqlc.arg(tenant_id),sqlc.arg(case_id),sqlc.arg(sales_plan_id),
 'CS-'||to_char(current_date,'YYYYMMDD')||'-'||lpad(nextval('sourcing_customer_selection_no_seq')::text,6,'0'),
 (SELECT coalesce(max(version_no),0)+1 FROM sourcing_customer_selections
  WHERE tenant_id=sqlc.arg(tenant_id) AND case_id=sqlc.arg(case_id)),
 sqlc.arg(requirement_version_no),'INTENT_RECHECK_PENDING',sqlc.arg(customer_contact),
 sqlc.arg(confirmation_note),sqlc.arg(customer_confirmed_at)::text::timestamptz,
 sqlc.arg(created_by),sqlc.arg(created_by_name))
RETURNING id;

-- name: CreateCustomerSelectionItem :one
INSERT INTO sourcing_customer_selection_items(tenant_id,selection_id,sales_plan_item_id,sourcing_line_id,
 procurement_plan_item_id,supplier_quote_line_id,shipping_plan_item_id,shipping_option_line_id,
 product_name,confirmed_qty,uom_code,customer_currency,customer_unit_price,promised_delivery_date,line_note,
 supplier_id,supplier_name,factory_id,factory_name,shipment_group_key,customer_managed_shipping)
VALUES(sqlc.arg(tenant_id),sqlc.arg(selection_id),sqlc.arg(sales_plan_item_id),sqlc.arg(sourcing_line_id),
 sqlc.arg(procurement_plan_item_id),sqlc.arg(supplier_quote_line_id),
 NULL,NULL,
 sqlc.arg(product_name),sqlc.arg(confirmed_qty)::text::numeric,sqlc.arg(uom_code),
 sqlc.arg(customer_currency),sqlc.arg(customer_unit_price)::text::numeric,
 nullif(sqlc.arg(promised_delivery_date)::text,'')::date,sqlc.arg(line_note),
 sqlc.arg(supplier_id),sqlc.arg(supplier_name),sqlc.arg(factory_id),sqlc.arg(factory_name),
 sqlc.arg(shipment_group_key),sqlc.arg(customer_managed_shipping))
RETURNING id;

-- name: CreateCustomerSelectionShipment :one
INSERT INTO sourcing_customer_selection_shipments(tenant_id,selection_id,shipment_group_key,
 sales_shipping_option_id,shipping_option_id,carrier_forwarder,service_option_name,
 shipping_employee_id,shipping_employee_name,customer_currency,customer_freight_amount,charge_basis,
 port_of_loading,port_of_discharge,estimated_departure,estimated_arrival,valid_until,customer_note)
VALUES(sqlc.arg(tenant_id),sqlc.arg(selection_id),sqlc.arg(shipment_group_key),
 sqlc.arg(sales_shipping_option_id),sqlc.arg(shipping_option_id),sqlc.arg(carrier_forwarder),
 sqlc.arg(service_option_name),sqlc.arg(shipping_employee_id),sqlc.arg(shipping_employee_name),
 sqlc.arg(customer_currency),sqlc.arg(customer_freight_amount)::text::numeric,sqlc.arg(charge_basis),
 sqlc.arg(port_of_loading),sqlc.arg(port_of_discharge),nullif(sqlc.arg(estimated_departure)::text,'')::date,
 nullif(sqlc.arg(estimated_arrival)::text,'')::date,nullif(sqlc.arg(valid_until)::text,'')::date,
 sqlc.arg(customer_note)) RETURNING id;

-- name: LinkCustomerSelectionShipmentItem :exec
INSERT INTO sourcing_customer_selection_shipment_items(tenant_id,selection_shipment_id,selection_item_id)
VALUES($1,$2,$3);

-- name: CreateFinalProcurementRecheckTask :exec
INSERT INTO sourcing_final_recheck_tasks(tenant_id,selection_id,selection_item_id,task_domain,procurement_rework_id)
VALUES($1,$2,$3,'PROCUREMENT',$4);

-- name: CreateFinalShippingRecheckTask :exec
INSERT INTO sourcing_final_recheck_tasks(tenant_id,selection_id,selection_item_id,task_domain,shipping_rework_id)
VALUES($1,$2,$3,'SHIPPING',$4);

-- name: CreateFinalShipmentRecheckTask :exec
INSERT INTO sourcing_final_recheck_tasks(tenant_id,selection_id,selection_shipment_id,task_domain,shipping_rework_id)
VALUES($1,$2,$3,'SHIPPING',$4);

-- name: ListCustomerSelections :many
SELECT id,case_id,sales_plan_id,selection_no,version_no,requirement_version_no,status,
 customer_contact,confirmation_note,customer_confirmed_at,created_by,created_by_name,created_at,
 coalesce(final_rechecked_at::text,'')::text AS final_rechecked_at,
 coalesce(customer_decided_at::text,'')::text AS customer_decided_at,customer_decision_note,
 coalesce(invalidated_at::text,'')::text AS invalidated_at,invalidated_reason
FROM sourcing_customer_selections WHERE tenant_id=$1 AND case_id=$2 ORDER BY version_no DESC;

-- name: ListCustomerSelectionItems :many
SELECT id,sales_plan_item_id,sourcing_line_id,procurement_plan_item_id,supplier_quote_line_id,
 coalesce(shipping_plan_item_id,0)::bigint AS shipping_plan_item_id,
 coalesce(shipping_option_line_id,0)::bigint AS shipping_option_line_id,
 product_name,confirmed_qty::text,uom_code,customer_currency,customer_unit_price::text,
 coalesce(promised_delivery_date::text,'')::text AS promised_delivery_date,line_note,
 supplier_id,supplier_name,factory_id,factory_name,shipment_group_key,customer_managed_shipping,
 coalesce(final_customer_currency,'')::text AS final_customer_currency,
 coalesce(final_customer_unit_price::text,'')::text AS final_customer_unit_price
FROM sourcing_customer_selection_items WHERE tenant_id=$1 AND selection_id=$2 ORDER BY id;

-- name: ListCustomerSelectionShipments :many
SELECT id,shipment_group_key,sales_shipping_option_id,shipping_option_id,carrier_forwarder,
 service_option_name,shipping_employee_id,shipping_employee_name,customer_currency,
 customer_freight_amount::text,charge_basis,port_of_loading,port_of_discharge,
 coalesce(estimated_departure::text,'')::text AS estimated_departure,
 coalesce(estimated_arrival::text,'')::text AS estimated_arrival,
 coalesce(valid_until::text,'')::text AS valid_until,customer_note,
 coalesce(final_customer_currency,'')::text AS final_customer_currency,
 coalesce(final_customer_freight_amount::text,'')::text AS final_customer_freight_amount
FROM sourcing_customer_selection_shipments
WHERE tenant_id=$1 AND selection_id=$2 ORDER BY id;

-- name: ListCustomerSelectionShipmentItemIDs :many
SELECT selection_item_id FROM sourcing_customer_selection_shipment_items
WHERE tenant_id=$1 AND selection_shipment_id=$2 ORDER BY selection_item_id;

-- name: ListFinalRecheckTasks :many
SELECT id,coalesce(selection_item_id,0)::bigint AS selection_item_id,
 coalesce(selection_shipment_id,0)::bigint AS selection_shipment_id,
 task_domain,coalesce(procurement_rework_id,0)::bigint AS procurement_rework_id,
 coalesce(shipping_rework_id,0)::bigint AS shipping_rework_id,status,
 coalesce(resolved_at::text,'')::text AS resolved_at,result_note,
 coalesce(resolved_by,0)::bigint AS resolved_by,resolved_by_name,
 coalesce(final_currency,'')::text AS final_currency,
 coalesce(final_unit_price::text,'')::text AS final_unit_price,
 coalesce(final_available_qty::text,'')::text AS final_available_qty,
 coalesce(final_lead_time,0)::int AS final_lead_time,
 coalesce(final_delivery_date::text,'')::text AS final_delivery_date,
 final_payment_terms,final_incoterm,coalesce(final_valid_until::text,'')::text AS final_valid_until,
 coalesce(final_freight_amount::text,'')::text AS final_freight_amount,
 coalesce(final_estimated_departure::text,'')::text AS final_estimated_departure,
 coalesce(final_estimated_arrival::text,'')::text AS final_estimated_arrival
FROM sourcing_final_recheck_tasks WHERE tenant_id=$1 AND selection_id=$2 ORDER BY selection_item_id,task_domain;

-- name: GetFinalTaskByProcurementRework :one
SELECT id FROM sourcing_final_recheck_tasks WHERE tenant_id=$1 AND procurement_rework_id=$2 AND status='OPEN';

-- name: ResolveFinalTaskByProcurementRework :exec
UPDATE sourcing_final_recheck_tasks SET status='RESOLVED',resolved_at=now(),result_note=sqlc.arg(result_note),
 resolved_by=sqlc.arg(resolved_by),resolved_by_name=sqlc.arg(resolved_by_name),final_currency=sqlc.arg(final_currency),
 final_unit_price=sqlc.arg(final_unit_price)::text::numeric,final_available_qty=sqlc.arg(final_available_qty)::text::numeric,
 final_lead_time=sqlc.arg(final_lead_time),final_delivery_date=sqlc.arg(final_delivery_date)::text::date,
 final_payment_terms=sqlc.arg(final_payment_terms),final_incoterm=sqlc.arg(final_incoterm),
 final_valid_until=sqlc.arg(final_valid_until)::text::date
WHERE tenant_id=sqlc.arg(tenant_id) AND procurement_rework_id=sqlc.arg(procurement_rework_id) AND status='OPEN';

-- name: GetFinalTaskByShippingRework :one
SELECT id FROM sourcing_final_recheck_tasks WHERE tenant_id=$1 AND shipping_rework_id=$2 AND status='OPEN';

-- name: ResolveFinalTaskByShippingRework :exec
UPDATE sourcing_final_recheck_tasks SET status='RESOLVED',resolved_at=now(),result_note=sqlc.arg(result_note),
 resolved_by=sqlc.arg(resolved_by),resolved_by_name=sqlc.arg(resolved_by_name),final_currency=sqlc.arg(final_currency),
 final_freight_amount=sqlc.arg(final_freight_amount)::text::numeric,
 final_estimated_departure=sqlc.arg(final_estimated_departure)::text::date,
 final_estimated_arrival=sqlc.arg(final_estimated_arrival)::text::date,
 final_valid_until=sqlc.arg(final_valid_until)::text::date
WHERE tenant_id=sqlc.arg(tenant_id) AND shipping_rework_id=sqlc.arg(shipping_rework_id) AND status='OPEN';

-- name: CompleteReadyCustomerSelections :exec
UPDATE sourcing_customer_selections s
SET status='AWAITING_CUSTOMER_CONFIRMATION',final_rechecked_at=now(),updated_at=now()
WHERE s.tenant_id=$1 AND s.status='INTENT_RECHECK_PENDING'
  AND NOT EXISTS (SELECT 1 FROM sourcing_final_recheck_tasks t
                  WHERE t.tenant_id=s.tenant_id AND t.selection_id=s.id AND t.status='OPEN');

-- name: AcceptCustomerSelection :execrows
UPDATE sourcing_customer_selections SET status='CUSTOMER_CONFIRMED',customer_contact=sqlc.arg(customer_contact),
 customer_decided_at=sqlc.arg(customer_decided_at)::text::timestamptz,customer_decision_note=sqlc.arg(decision_note),updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND status='AWAITING_CUSTOMER_CONFIRMATION';

-- name: RejectCustomerSelection :execrows
UPDATE sourcing_customer_selections SET status='CUSTOMER_REJECTED',customer_contact=sqlc.arg(customer_contact),
 customer_decided_at=sqlc.arg(customer_decided_at)::text::timestamptz,customer_decision_note=sqlc.arg(decision_note),updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND status='AWAITING_CUSTOMER_CONFIRMATION';

-- name: SetFinalCustomerItemPrice :exec
UPDATE sourcing_customer_selection_items SET final_customer_currency=sqlc.arg(final_customer_currency),final_customer_unit_price=sqlc.arg(final_customer_unit_price)::text::numeric
WHERE tenant_id=sqlc.arg(tenant_id) AND selection_id=sqlc.arg(selection_id) AND id=sqlc.arg(id);

-- name: SetFinalCustomerShipmentPrice :exec
UPDATE sourcing_customer_selection_shipments SET final_customer_currency=sqlc.arg(final_customer_currency),final_customer_freight_amount=sqlc.arg(final_customer_freight_amount)::text::numeric
WHERE tenant_id=sqlc.arg(tenant_id) AND selection_id=sqlc.arg(selection_id) AND id=sqlc.arg(id);
