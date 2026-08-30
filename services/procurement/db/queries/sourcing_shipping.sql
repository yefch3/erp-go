-- name: UpsertSourcingShippingRequest :exec
INSERT INTO sourcing_shipping_requests(
 tenant_id,case_id,requirement_version_no,status,case_no,case_title,customer_id,customer_name,
 sales_employee_id,sales_employee_name,destination_port,cargo_summary,requested_by,requested_by_name)
SELECT c.tenant_id,c.id,greatest(c.requirement_version_no,1),'WAITING_PARTICIPATION',c.case_no,c.title,c.customer_id,c.customer_name,
 c.owner_id,c.owner_name,coalesce(max(nullif(l.port,'')),''),
 coalesce(string_agg(l.product
   || CASE WHEN l.quantity IS NULL THEN '' ELSE ' ' || l.quantity::text || ' ' || l.quantity_unit END
   || CASE WHEN trim(l.packaging)='' THEN '' ELSE '，包装 ' || l.packaging END
   || CASE WHEN trim(l.delivery)='' THEN '' ELSE '，时间要求 ' || l.delivery END,
   '；' ORDER BY l.line_no) FILTER (WHERE l.decision='CONFIRMED'),''),
 sqlc.arg(operator_id),sqlc.arg(operator_name)
FROM sourcing_cases c
LEFT JOIN sourcing_lines l ON l.tenant_id=c.tenant_id AND l.case_id=c.id
WHERE c.tenant_id=sqlc.arg(tenant_id) AND c.id=sqlc.arg(case_id)
GROUP BY c.tenant_id,c.id
ON CONFLICT (tenant_id,case_id) DO UPDATE SET
 requirement_version_no=excluded.requirement_version_no,
 status=CASE WHEN sourcing_shipping_requests.status='PLAN_SUBMITTED_TO_SALES' THEN 'REQUOTE_REQUIRED' ELSE sourcing_shipping_requests.status END,
 case_title=excluded.case_title,customer_id=excluded.customer_id,customer_name=excluded.customer_name,
 sales_employee_id=excluded.sales_employee_id,sales_employee_name=excluded.sales_employee_name,
 destination_port=excluded.destination_port,cargo_summary=excluded.cargo_summary,
 requested_by=excluded.requested_by,requested_by_name=excluded.requested_by_name,requested_at=now(),updated_at=now();

-- name: GetSourcingShippingRequest :one
SELECT * FROM sourcing_shipping_requests WHERE tenant_id=$1 AND case_id=$2;

-- name: ListSourcingShippingRequests :many
SELECT *,count(*) OVER() AS total FROM sourcing_shipping_requests
WHERE tenant_id=sqlc.arg(tenant_id)
  AND (sqlc.arg(status)::text='' OR status=sqlc.arg(status)::text)
  AND (sqlc.arg(keyword)::text='' OR case_no ILIKE '%'||sqlc.arg(keyword)::text||'%'
       OR case_title ILIKE '%'||sqlc.arg(keyword)::text||'%'
       OR customer_name ILIKE '%'||sqlc.arg(keyword)::text||'%'
       OR cargo_summary ILIKE '%'||sqlc.arg(keyword)::text||'%')
ORDER BY CASE status WHEN 'REQUOTE_REQUIRED' THEN 0 WHEN 'WAITING_PARTICIPATION' THEN 1 WHEN 'QUOTING' THEN 2 WHEN 'MANAGER_REVIEW' THEN 3 ELSE 4 END,updated_at DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: StartSourcingShippingRequest :execrows
UPDATE sourcing_shipping_requests SET status='QUOTING',updated_at=now()
WHERE tenant_id=$1 AND case_id=$2 AND status IN ('WAITING_PARTICIPATION','REQUOTE_REQUIRED');

-- name: CreateSourcingShippingOption :one
INSERT INTO sourcing_shipping_options(
 tenant_id,request_id,carrier_forwarder,port_of_loading,port_of_discharge,quoted_at,estimated_departure,
 estimated_arrival,valid_until,note,status,created_by,created_by_name,service_option_name,version_no)
VALUES(sqlc.arg(tenant_id),sqlc.arg(request_id),sqlc.arg(carrier_forwarder)::text,sqlc.arg(port_of_loading),
 sqlc.arg(port_of_discharge),nullif(sqlc.arg(quoted_at)::text,'')::date,
 nullif(sqlc.arg(estimated_departure)::text,'')::date,
 nullif(sqlc.arg(estimated_arrival)::text,'')::date,
 nullif(sqlc.arg(valid_until)::text,'')::date,sqlc.arg(note),'SUBMITTED',sqlc.arg(created_by),sqlc.arg(created_by_name),sqlc.arg(service_option_name)::text,
 (SELECT coalesce(max(version_no),0)+1 FROM sourcing_shipping_options
  WHERE tenant_id=sqlc.arg(tenant_id) AND request_id=sqlc.arg(request_id)
    AND created_by=sqlc.arg(created_by) AND lower(carrier_forwarder)=lower(sqlc.arg(carrier_forwarder)::text)
    AND lower(service_option_name)=lower(sqlc.arg(service_option_name)::text)))
RETURNING id;

-- name: CreateSourcingShippingOptionLine :exec
INSERT INTO sourcing_shipping_option_lines(
 tenant_id,option_id,sourcing_line_id,line_no,product_snapshot,specification_snapshot,quantity,quantity_unit,
 currency,charge_basis,unit_rate,total_freight,note)
VALUES(sqlc.arg(tenant_id),sqlc.arg(option_id),sqlc.arg(sourcing_line_id),sqlc.arg(line_no),
 sqlc.arg(product_snapshot),sqlc.arg(specification_snapshot),nullif(sqlc.arg(quantity)::text,'')::numeric,
 sqlc.arg(quantity_unit),upper(sqlc.arg(currency)::text)::char(3),sqlc.arg(charge_basis),
 sqlc.arg(unit_rate)::text::numeric,sqlc.arg(total_freight)::text::numeric,sqlc.arg(note));

-- name: MarkSourcingShippingLinked :exec
UPDATE sourcing_shipping_requests SET status='MANAGER_REVIEW',updated_at=now()
WHERE tenant_id=$1 AND id=$2;

-- name: ListSourcingShippingOptions :many
SELECT id,request_id,carrier_forwarder,port_of_loading,port_of_discharge,
 coalesce(quoted_at::text,'')::text AS quoted_at,
 coalesce(estimated_departure::text,'')::text AS estimated_departure,
 coalesce(estimated_arrival::text,'')::text AS estimated_arrival,
 coalesce(valid_until::text,'')::text AS valid_until,note,status,created_by,created_by_name,service_option_name,version_no,submitted_at,created_at
FROM sourcing_shipping_options WHERE tenant_id=$1 AND request_id=$2 AND status IN ('SUBMITTED','RETURNED')
ORDER BY created_at DESC,id DESC;

-- name: ListSourcingShippingOptionLines :many
SELECT id,option_id,sourcing_line_id,line_no,product_snapshot,specification_snapshot,
 coalesce(quantity::text,'')::text AS quantity,quantity_unit,currency,charge_basis,unit_rate::text,
 total_freight::text,note
FROM sourcing_shipping_option_lines
WHERE tenant_id=$1 AND option_id=$2
ORDER BY line_no,id;

-- name: ListSourcingShippingCargoItems :many
SELECT id AS sourcing_line_id,line_no,product,material_standard,grade,thickness,width,length_or_form,
 surface_requirement,packaging,delivery,coalesce(quantity::text,'')::text AS quantity,quantity_unit
FROM sourcing_lines
WHERE tenant_id=$1 AND case_id=$2 AND decision='CONFIRMED'
ORDER BY line_no;

-- name: ListSourcingShippingParticipants :many
SELECT id,request_id,employee_id,employee_name,participant_role,status,primary_requested_at,joined_at,updated_at
FROM sourcing_shipping_participants
WHERE tenant_id=$1 AND request_id=$2 AND status='ACTIVE'
ORDER BY CASE participant_role WHEN 'PRIMARY' THEN 0 ELSE 1 END,joined_at,id;

-- name: JoinSourcingShippingRequest :exec
INSERT INTO sourcing_shipping_participants(tenant_id,request_id,employee_id,employee_name)
VALUES($1,$2,$3,$4)
ON CONFLICT (tenant_id,request_id,employee_id) DO UPDATE SET
 status='ACTIVE',employee_name=excluded.employee_name,updated_at=now();

-- name: ShippingParticipant :one
SELECT id,request_id,employee_id,employee_name,participant_role,status,primary_requested_at,joined_at,updated_at
FROM sourcing_shipping_participants WHERE tenant_id=$1 AND request_id=$2 AND employee_id=$3 AND status='ACTIVE';

-- name: RequestPrimaryShipping :execrows
UPDATE sourcing_shipping_participants SET primary_requested_at=now(),updated_at=now()
WHERE tenant_id=$1 AND request_id=$2 AND employee_id=$3 AND status='ACTIVE' AND participant_role<>'PRIMARY';

-- name: ClearPrimaryShipping :exec
UPDATE sourcing_shipping_participants SET participant_role='COLLABORATOR',primary_requested_at=NULL,updated_at=now()
WHERE tenant_id=$1 AND request_id=$2 AND participant_role='PRIMARY' AND status='ACTIVE';

-- name: AssignPrimaryShipping :execrows
UPDATE sourcing_shipping_participants SET participant_role='PRIMARY',primary_requested_at=NULL,updated_at=now()
WHERE tenant_id=$1 AND request_id=$2 AND employee_id=$3 AND status='ACTIVE';

-- name: SourcingShippingPlanCandidate :one
SELECT ol.id AS option_line_id,ol.sourcing_line_id,o.request_id,o.carrier_forwarder,o.service_option_name,o.created_by AS shipping_employee_id,
 o.created_by_name AS shipping_employee_name,o.version_no AS quote_version_no,o.port_of_loading,o.port_of_discharge,
 coalesce(o.estimated_departure::text,'')::text AS estimated_departure,
 coalesce(o.estimated_arrival::text,'')::text AS estimated_arrival,
 coalesce(o.valid_until::text,'')::text AS valid_until,ol.product_snapshot AS product_name,
 ol.currency,ol.charge_basis,ol.unit_rate::text,ol.total_freight::text
FROM sourcing_shipping_option_lines ol
JOIN sourcing_shipping_options o ON o.id=ol.option_id AND o.tenant_id=ol.tenant_id
WHERE ol.tenant_id=$1 AND o.request_id=$2 AND ol.id=$3 AND o.status='SUBMITTED'
  AND (o.valid_until IS NULL OR o.valid_until>=current_date)
  AND NOT EXISTS (SELECT 1 FROM sourcing_shipping_options newer
    JOIN sourcing_shipping_option_lines newer_line ON newer_line.option_id=newer.id AND newer_line.sourcing_line_id=ol.sourcing_line_id
    WHERE newer.tenant_id=o.tenant_id AND newer.request_id=o.request_id AND newer.created_by=o.created_by
      AND lower(newer.carrier_forwarder)=lower(o.carrier_forwarder)
      AND lower(newer.service_option_name)=lower(o.service_option_name)
      AND newer.version_no>o.version_no AND newer.status='SUBMITTED');

-- name: CreateSourcingShippingPlan :one
INSERT INTO sourcing_shipping_plans(tenant_id,request_id,plan_no,version_no,requirement_version_no,manager_note,
 created_by,created_by_name,target_sales_id,target_sales_name)
VALUES(sqlc.arg(tenant_id),sqlc.arg(request_id),
 'SP-'||to_char(current_date,'YYYYMMDD')||'-'||lpad(nextval('sourcing_shipping_plan_no_seq')::text,6,'0'),
 (SELECT coalesce(max(version_no),0)+1 FROM sourcing_shipping_plans WHERE tenant_id=sqlc.arg(tenant_id) AND request_id=sqlc.arg(request_id)),
 sqlc.arg(requirement_version_no),sqlc.arg(manager_note),sqlc.arg(created_by),sqlc.arg(created_by_name),
 sqlc.arg(target_sales_id),sqlc.arg(target_sales_name))
RETURNING id,plan_no,version_no;

-- name: CreateSourcingShippingPlanItem :exec
INSERT INTO sourcing_shipping_plan_items(tenant_id,plan_id,sourcing_line_id,shipping_option_line_id,selection_type,priority,
 reason,risk,carrier_forwarder,service_option_name,shipping_employee_id,shipping_employee_name,product_name,currency,charge_basis,unit_rate,
 total_freight,port_of_loading,port_of_discharge,estimated_departure,estimated_arrival,valid_until,quote_version_no)
VALUES(sqlc.arg(tenant_id),sqlc.arg(plan_id),sqlc.arg(sourcing_line_id),sqlc.arg(shipping_option_line_id),
 sqlc.arg(selection_type),sqlc.arg(priority),sqlc.arg(reason),sqlc.arg(risk),sqlc.arg(carrier_forwarder),sqlc.arg(service_option_name),
 sqlc.arg(shipping_employee_id),sqlc.arg(shipping_employee_name),sqlc.arg(product_name),sqlc.arg(currency),
 sqlc.arg(charge_basis),sqlc.arg(unit_rate)::text::numeric,sqlc.arg(total_freight)::text::numeric,
 sqlc.arg(port_of_loading),sqlc.arg(port_of_discharge),nullif(sqlc.arg(estimated_departure)::text,'')::date,
 nullif(sqlc.arg(estimated_arrival)::text,'')::date,nullif(sqlc.arg(valid_until)::text,'')::date,sqlc.arg(quote_version_no));

-- name: SupersedeConfirmedSourcingShippingPlans :exec
UPDATE sourcing_shipping_plans SET status='SUPERSEDED',updated_at=now()
WHERE tenant_id=$1 AND request_id=$2 AND status='CONFIRMED';

-- name: MarkSourcingShippingPlanReady :exec
UPDATE sourcing_shipping_requests SET status='PLAN_READY',updated_at=now() WHERE tenant_id=$1 AND id=$2;

-- name: ListSourcingShippingPlans :many
SELECT id,request_id,plan_no,version_no,requirement_version_no,status,manager_note,created_by,created_by_name,
 confirmed_at,coalesce(submitted_to_sales_by,0)::bigint AS submitted_to_sales_by,submitted_to_sales_by_name,
 submitted_to_sales_at,target_sales_id,target_sales_name,created_at
FROM sourcing_shipping_plans WHERE tenant_id=$1 AND request_id=$2 ORDER BY version_no DESC;

-- name: ListSourcingShippingPlanItems :many
SELECT id,sourcing_line_id,shipping_option_line_id,selection_type,priority,reason,risk,carrier_forwarder,service_option_name,
 shipping_employee_id,shipping_employee_name,product_name,currency,charge_basis,unit_rate::text,total_freight::text,
 port_of_loading,port_of_discharge,coalesce(estimated_departure::text,'')::text AS estimated_departure,
 coalesce(estimated_arrival::text,'')::text AS estimated_arrival,coalesce(valid_until::text,'')::text AS valid_until,quote_version_no
FROM sourcing_shipping_plan_items WHERE tenant_id=$1 AND plan_id=$2
ORDER BY sourcing_line_id,selection_type DESC,priority,id;

-- name: GetSourcingShippingPlan :one
SELECT id,request_id,plan_no,version_no,requirement_version_no,status,manager_note,created_by,created_by_name,
 confirmed_at,coalesce(submitted_to_sales_by,0)::bigint AS submitted_to_sales_by,submitted_to_sales_by_name,
 submitted_to_sales_at,target_sales_id,target_sales_name,created_at
FROM sourcing_shipping_plans WHERE tenant_id=$1 AND id=$2;

-- name: SubmitSourcingShippingPlanToSales :execrows
UPDATE sourcing_shipping_plans SET status='SUBMITTED_TO_SALES',submitted_to_sales_by=sqlc.arg(operator_id),
 submitted_to_sales_by_name=sqlc.arg(operator_name),submitted_to_sales_at=now(),updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND status='CONFIRMED';

-- name: MarkSourcingShippingPlanSubmitted :exec
UPDATE sourcing_shipping_requests SET status='PLAN_SUBMITTED_TO_SALES',updated_at=now()
WHERE tenant_id=$1 AND id=$2;
