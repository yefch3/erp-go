-- name: ProcurementPlanCase :one
SELECT id,owner_id,owner_name,status,handoff_status,requirement_version_no
FROM sourcing_cases WHERE tenant_id=$1 AND id=$2;

-- name: CountProcurementPlanLines :one
SELECT count(*) FROM sourcing_lines
WHERE tenant_id=$1 AND case_id=$2 AND decision NOT IN ('SKIPPED','NO_MATCH');

-- name: ProcurementPlanCandidate :one
SELECT ql.id AS quote_line_id,ql.sourcing_line_id,ql.qty::text,ql.unit_price::text,
 coalesce(ql.moq::text,'')::text AS moq,ql.lead_time,q.currency,q.payment_terms,q.incoterm,
 coalesce(q.valid_until::text,'')::text AS valid_until,q.version_no AS quote_version_no,
 q.created_by AS buyer_id,r.created_by_name AS buyer_name,
 r.supplier_id,r.supplier_name,coalesce(r.factory_id,0)::bigint AS factory_id,r.factory_name,
 sl.product AS product_name,fl.uom_code,q.confirmation_status,
 coalesce(q.valid_until<current_date,false)::boolean AS quote_expired,
 EXISTS (
   SELECT 1 FROM supplier_quotes newer
   JOIN supplier_quote_lines newer_line ON newer_line.supplier_quote_id=newer.id
     AND newer_line.tenant_id=newer.tenant_id AND newer_line.sourcing_line_id=ql.sourcing_line_id
   WHERE newer.tenant_id=q.tenant_id AND newer.factory_rfq_id=q.factory_rfq_id
     AND newer.version_no>q.version_no AND newer.confirmation_status='WRITTEN_CONFIRMED'
 ) AS newer_quote_exists,
 EXISTS (
   SELECT 1 FROM procurement_rework_requests returned
   WHERE returned.tenant_id=ql.tenant_id
     AND returned.supplier_quote_line_id=ql.id
     AND returned.request_type IN ('REQUOTE','RENEGOTIATE')
 ) AS quote_returned
FROM supplier_quote_lines ql
JOIN supplier_quotes q ON q.id=ql.supplier_quote_id AND q.tenant_id=ql.tenant_id
JOIN factory_rfqs r ON r.id=q.factory_rfq_id AND r.tenant_id=q.tenant_id
JOIN factory_rfq_lines fl ON fl.factory_rfq_id=r.id AND fl.sourcing_line_id=ql.sourcing_line_id AND fl.tenant_id=ql.tenant_id
JOIN sourcing_lines sl ON sl.id=ql.sourcing_line_id AND sl.tenant_id=ql.tenant_id
WHERE ql.tenant_id=sqlc.arg(tenant_id) AND r.case_id=sqlc.arg(case_id) AND ql.id=sqlc.arg(id);

-- name: CreateProcurementPlan :one
INSERT INTO procurement_plans(tenant_id,case_id,plan_no,version_no,requirement_version_no,status,manager_note,
 created_by,created_by_name,confirmed_by,confirmed_by_name,target_sales_id,target_sales_name)
VALUES(sqlc.arg(tenant_id),sqlc.arg(case_id),
 'PP-'||to_char(current_date,'YYYYMMDD')||'-'||lpad(nextval('procurement_plan_no_seq')::text,6,'0'),
 (SELECT coalesce(max(version_no),0)+1 FROM procurement_plans
  WHERE tenant_id=sqlc.arg(tenant_id) AND case_id=sqlc.arg(case_id)),
 sqlc.arg(requirement_version_no),'CONFIRMED',sqlc.arg(manager_note),
 sqlc.arg(created_by),sqlc.arg(created_by_name),sqlc.arg(created_by),sqlc.arg(created_by_name),
 sqlc.arg(target_sales_id),sqlc.arg(target_sales_name))
RETURNING id,plan_no,version_no;

-- name: CreateProcurementPlanItem :exec
INSERT INTO procurement_plan_items(tenant_id,plan_id,sourcing_line_id,supplier_quote_line_id,selection_type,priority,reason,risk,
 supplier_id,supplier_name,factory_id,factory_name,buyer_id,buyer_name,product_name,currency,unit_price,available_qty,uom_code,
 moq,lead_time,payment_terms,incoterm,valid_until,quote_version_no)
VALUES(sqlc.arg(tenant_id),sqlc.arg(plan_id),sqlc.arg(sourcing_line_id),sqlc.arg(supplier_quote_line_id),
 sqlc.arg(selection_type),sqlc.arg(priority),sqlc.arg(reason),sqlc.arg(risk),sqlc.arg(supplier_id),sqlc.arg(supplier_name),
 nullif(sqlc.arg(factory_id)::bigint,0),sqlc.arg(factory_name),sqlc.arg(buyer_id),sqlc.arg(buyer_name),sqlc.arg(product_name),
 sqlc.arg(currency),sqlc.arg(unit_price)::text::numeric,sqlc.arg(available_qty)::text::numeric,sqlc.arg(uom_code),
 nullif(sqlc.arg(moq)::text,'')::numeric,sqlc.arg(lead_time),sqlc.arg(payment_terms),sqlc.arg(incoterm),
 nullif(sqlc.arg(valid_until)::text,'')::date,sqlc.arg(quote_version_no));

-- name: SupersedeConfirmedProcurementPlans :exec
UPDATE procurement_plans SET status='SUPERSEDED',updated_at=now()
WHERE tenant_id=$1 AND case_id=$2 AND status='CONFIRMED';

-- name: MarkSourcingCaseProcurementPlanReady :exec
UPDATE sourcing_cases SET handoff_status='PROCUREMENT_PLAN_READY',updated_at=now()
WHERE tenant_id=$1 AND id=$2;

-- name: ListProcurementPlans :many
SELECT id,case_id,plan_no,version_no,requirement_version_no,status,manager_note,
 created_by_name,confirmed_by_name,confirmed_at,submitted_to_sales_by_name,submitted_to_sales_at,
 target_sales_id,target_sales_name,created_at
FROM procurement_plans WHERE tenant_id=$1 AND case_id=$2 ORDER BY version_no DESC;

-- name: GetProcurementPlan :one
SELECT id,case_id,plan_no,version_no,requirement_version_no,status,manager_note,
 created_by_name,confirmed_by_name,confirmed_at,submitted_to_sales_by_name,submitted_to_sales_at,
 target_sales_id,target_sales_name,created_at
FROM procurement_plans WHERE tenant_id=$1 AND id=$2;

-- name: ListProcurementPlanItems :many
SELECT id,sourcing_line_id,supplier_quote_line_id,selection_type,priority,reason,risk,
 supplier_id,supplier_name,coalesce(factory_id,0)::bigint AS factory_id,factory_name,buyer_id,buyer_name,
 product_name,currency,unit_price::text,available_qty::text,uom_code,coalesce(moq::text,'')::text AS moq,
 coalesce(lead_time,0)::int AS lead_time,payment_terms,incoterm,coalesce(valid_until::text,'')::text AS valid_until,
 quote_version_no
FROM procurement_plan_items WHERE tenant_id=$1 AND plan_id=$2
ORDER BY sourcing_line_id,selection_type DESC,priority,id;

-- name: SubmitProcurementPlanToSales :execrows
UPDATE procurement_plans SET status='SUBMITTED_TO_SALES',submitted_to_sales_by=sqlc.arg(operator_id),
 submitted_to_sales_by_name=sqlc.arg(operator_name),submitted_to_sales_at=now(),updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND status='CONFIRMED';

-- name: MarkSourcingCaseProcurementPlanSubmitted :exec
UPDATE sourcing_cases SET handoff_status='PROCUREMENT_PLAN_SUBMITTED',updated_at=now()
WHERE tenant_id=$1 AND id=$2;

-- name: ProcurementReworkQuote :one
SELECT ql.id AS quote_line_id,ql.sourcing_line_id,q.created_by AS buyer_id,r.created_by_name AS buyer_name,
 r.supplier_id,r.supplier_name,sl.product AS product_name
FROM supplier_quote_lines ql
JOIN supplier_quotes q ON q.id=ql.supplier_quote_id AND q.tenant_id=ql.tenant_id
JOIN factory_rfqs r ON r.id=q.factory_rfq_id AND r.tenant_id=q.tenant_id
JOIN sourcing_lines sl ON sl.id=ql.sourcing_line_id AND sl.tenant_id=ql.tenant_id
WHERE ql.tenant_id=$1 AND r.case_id=$2 AND ql.id=$3;

-- name: ProcurementReworkProduct :one
SELECT id,product FROM sourcing_lines WHERE tenant_id=$1 AND case_id=$2 AND id=$3;

-- name: CreateProcurementReworkRequest :one
INSERT INTO procurement_rework_requests(tenant_id,case_id,plan_id,sourcing_line_id,supplier_quote_line_id,
 request_type,scope_type,assigned_buyer_id,assigned_buyer_name,supplier_id,supplier_name,product_name,reason,
 created_by,created_by_name)
VALUES(sqlc.arg(tenant_id),sqlc.arg(case_id),nullif(sqlc.arg(plan_id)::bigint,0),
 nullif(sqlc.arg(sourcing_line_id)::bigint,0),nullif(sqlc.arg(supplier_quote_line_id)::bigint,0),
 sqlc.arg(request_type),sqlc.arg(scope_type),nullif(sqlc.arg(assigned_buyer_id)::bigint,0),
 sqlc.arg(assigned_buyer_name),nullif(sqlc.arg(supplier_id)::bigint,0),sqlc.arg(supplier_name),
 sqlc.arg(product_name),sqlc.arg(reason),sqlc.arg(created_by),sqlc.arg(created_by_name))
RETURNING id;

-- name: ListProcurementReworkRequests :many
SELECT rr.id,rr.case_id,coalesce(rr.plan_id,0)::bigint AS plan_id,coalesce(rr.sourcing_line_id,0)::bigint AS sourcing_line_id,
 coalesce(rr.supplier_quote_line_id,0)::bigint AS supplier_quote_line_id,rr.request_type,rr.scope_type,
 coalesce(rr.assigned_buyer_id,0)::bigint AS assigned_buyer_id,rr.assigned_buyer_name,
 coalesce(rr.supplier_id,0)::bigint AS supplier_id,rr.supplier_name,rr.product_name,rr.reason,rr.status,
 rr.created_by_name,rr.created_at,rr.resolved_by_name,rr.resolved_at,rr.resolution_note,
 coalesce(ft.id,0)::bigint AS final_recheck_task_id
FROM procurement_rework_requests rr
LEFT JOIN sourcing_final_recheck_tasks ft ON ft.tenant_id=rr.tenant_id AND ft.procurement_rework_id=rr.id
WHERE rr.tenant_id=$1 AND rr.case_id=$2 ORDER BY rr.created_at DESC;

-- name: GetProcurementReworkRequest :one
SELECT id,case_id,coalesce(assigned_buyer_id,0)::bigint AS assigned_buyer_id,status
FROM procurement_rework_requests WHERE tenant_id=$1 AND id=$2;

-- name: ResolveProcurementReworkRequest :execrows
UPDATE procurement_rework_requests SET status='RESOLVED',resolved_by=sqlc.arg(operator_id),
 resolved_by_name=sqlc.arg(operator_name),resolved_at=now(),resolution_note=sqlc.arg(resolution_note)
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND status='OPEN';
