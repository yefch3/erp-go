-- name: CostScenarioCase :one
SELECT id,customer_id,customer_name,contact_name,contact_email,status
FROM sourcing_cases WHERE tenant_id=$1 AND id=$2;

-- name: CostScenarioCandidate :one
SELECT ql.id AS quote_line_id,ql.sourcing_line_id,ql.qty::text,ql.unit_price::text,
 q.currency,r.supplier_name,sl.product_id,sl.sku_id,sl.product,fl.spec_snapshot,fl.uom_code
FROM supplier_quote_lines ql
JOIN supplier_quotes q ON q.id=ql.supplier_quote_id AND q.tenant_id=ql.tenant_id
JOIN factory_rfqs r ON r.id=q.factory_rfq_id AND r.tenant_id=q.tenant_id
JOIN factory_rfq_lines fl ON fl.factory_rfq_id=r.id AND fl.sourcing_line_id=ql.sourcing_line_id AND fl.tenant_id=ql.tenant_id
JOIN sourcing_lines sl ON sl.id=ql.sourcing_line_id AND sl.tenant_id=ql.tenant_id
WHERE ql.tenant_id=$1 AND r.case_id=$2 AND ql.id=$3
  AND q.confirmation_status='WRITTEN_CONFIRMED';

-- name: CountConfirmedSourcingLines :one
-- 进入询价后的历史数据可能仍保留 PENDING；只有明确忽略或不匹配的产品不参与成本方案。
SELECT count(*) FROM sourcing_lines
WHERE tenant_id=$1 AND case_id=$2 AND decision NOT IN ('SKIPPED','NO_MATCH');

-- name: CostScenarioTerms :one
SELECT incoterm,port,payment_terms FROM sourcing_lines
WHERE tenant_id=$1 AND case_id=$2 AND decision NOT IN ('SKIPPED','NO_MATCH') ORDER BY line_no LIMIT 1;

-- name: CreateCostScenario :one
INSERT INTO cost_scenarios(tenant_id,case_id,scenario_no,currency,allocation_basis,margin_type,margin_value,
 fx_rate,fx_rate_at,fx_source,fx_base_currency,product_total,charge_total,landed_total,margin_total,customer_total,
 created_by,created_by_name)
VALUES(sqlc.arg(tenant_id),sqlc.arg(case_id),
 'CS-'||to_char(current_date,'YYYYMMDD')||'-'||lpad(nextval('cost_scenario_no_seq')::text,6,'0'),
 sqlc.arg(currency),sqlc.arg(allocation_basis),sqlc.arg(margin_type),sqlc.arg(margin_value)::text::numeric,
 sqlc.arg(fx_rate)::text::numeric,sqlc.arg(fx_rate_at)::timestamptz,sqlc.arg(fx_source),sqlc.arg(fx_base_currency),
 sqlc.arg(product_total)::text::numeric,sqlc.arg(charge_total)::text::numeric,sqlc.arg(landed_total)::text::numeric,
 sqlc.arg(margin_total)::text::numeric,sqlc.arg(customer_total)::text::numeric,sqlc.arg(created_by),sqlc.arg(created_by_name))
RETURNING id,scenario_no;

-- name: CreateCostCharge :exec
INSERT INTO cost_charges(tenant_id,scenario_id,charge_type,basis,description,origin_port,destination_port,container_type,
 amount,currency,converted_amount,source_fx_rate,target_fx_rate,fx_rate_at,fx_source,effective_at,valid_until,source,remark)
VALUES(sqlc.arg(tenant_id),sqlc.arg(scenario_id),sqlc.arg(charge_type),sqlc.arg(basis),sqlc.arg(description),
 sqlc.arg(origin_port),sqlc.arg(destination_port),sqlc.arg(container_type),sqlc.arg(amount)::text::numeric,
 sqlc.arg(currency),sqlc.arg(converted_amount)::text::numeric,sqlc.arg(source_fx_rate)::text::numeric,
 sqlc.arg(target_fx_rate)::text::numeric,sqlc.arg(fx_rate_at)::timestamptz,sqlc.arg(fx_source),
 nullif(sqlc.arg(effective_at)::text,'')::date,nullif(sqlc.arg(valid_until)::text,'')::date,sqlc.arg(source),sqlc.arg(remark));

-- name: CreateCostScenarioLine :exec
INSERT INTO cost_scenario_lines(tenant_id,scenario_id,sourcing_line_id,supplier_quote_line_id,supplier_name,
 product_id,sku_id,product_name,spec_snapshot,qty,uom_code,source_currency,source_unit_price,source_fx_rate,target_fx_rate,
 product_cost,allocated_charge,landed_cost,margin_amount,customer_unit_price,customer_amount)
VALUES(sqlc.arg(tenant_id),sqlc.arg(scenario_id),sqlc.arg(sourcing_line_id),sqlc.arg(supplier_quote_line_id),sqlc.arg(supplier_name),
 sqlc.arg(product_id),nullif(sqlc.arg(sku_id)::bigint,0),sqlc.arg(product_name),sqlc.arg(spec_snapshot),sqlc.arg(qty)::text::numeric,
 sqlc.arg(uom_code),sqlc.arg(source_currency),sqlc.arg(source_unit_price)::text::numeric,sqlc.arg(source_fx_rate)::text::numeric,
 sqlc.arg(target_fx_rate)::text::numeric,sqlc.arg(product_cost)::text::numeric,sqlc.arg(allocated_charge)::text::numeric,
 sqlc.arg(landed_cost)::text::numeric,sqlc.arg(margin_amount)::text::numeric,sqlc.arg(customer_unit_price)::text::numeric,
 sqlc.arg(customer_amount)::text::numeric);

-- name: ListCostScenarios :many
SELECT id,case_id,scenario_no,currency,allocation_basis,margin_type,margin_value::text,fx_rate::text,fx_rate_at,
 fx_source,fx_base_currency,product_total::text,charge_total::text,landed_total::text,margin_total::text,
 customer_total::text,status,coalesce(customer_quotation_id,0)::bigint AS customer_quotation_id,customer_quote_no,
 created_by_name,confirmed_by_name,confirmed_at,confirm_reason,created_at
FROM cost_scenarios WHERE tenant_id=$1 AND case_id=$2 ORDER BY created_at DESC;

-- name: GetCostScenario :one
SELECT id,case_id,scenario_no,currency,allocation_basis,margin_type,margin_value::text,fx_rate::text,fx_rate_at,
 fx_source,fx_base_currency,product_total::text,charge_total::text,landed_total::text,margin_total::text,
 customer_total::text,status,coalesce(customer_quotation_id,0)::bigint AS customer_quotation_id,customer_quote_no,
 created_by_name,confirmed_by_name,confirmed_at,confirm_reason,created_at
FROM cost_scenarios WHERE tenant_id=$1 AND id=$2;

-- name: ListCostCharges :many
SELECT id,charge_type,basis,description,origin_port,destination_port,container_type,amount::text,currency,
 converted_amount::text,source_fx_rate::text,target_fx_rate::text,fx_rate_at,fx_source,
 coalesce(effective_at::text,'')::text AS effective_at,coalesce(valid_until::text,'')::text AS valid_until,source,remark
FROM cost_charges WHERE tenant_id=$1 AND scenario_id=$2 ORDER BY id;

-- name: ListCostScenarioLines :many
SELECT id,sourcing_line_id,supplier_quote_line_id,supplier_name,product_id,coalesce(sku_id,0)::bigint AS sku_id,
 product_name,spec_snapshot,qty::text,uom_code,source_currency,source_unit_price::text,source_fx_rate::text,target_fx_rate::text,
 product_cost::text,allocated_charge::text,landed_cost::text,margin_amount::text,customer_unit_price::text,customer_amount::text
FROM cost_scenario_lines WHERE tenant_id=$1 AND scenario_id=$2 ORDER BY sourcing_line_id;

-- name: ConfirmCostScenario :execrows
UPDATE cost_scenarios SET status='CONFIRMED',confirmed_by=sqlc.arg(confirmed_by),confirmed_by_name=sqlc.arg(confirmed_by_name),
 confirm_reason=sqlc.arg(confirm_reason),
 confirmed_at=now(),updated_at=now() WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND status='DRAFT';

-- name: SupersedeOtherCostScenarios :exec
UPDATE cost_scenarios SET status='SUPERSEDED',updated_at=now()
WHERE tenant_id=$1 AND case_id=$2 AND id<>$3 AND status='CONFIRMED';

-- name: MarkSourcingCaseCosting :exec
UPDATE sourcing_cases SET status='COSTING',updated_at=now() WHERE tenant_id=$1 AND id=$2;

-- name: LinkCustomerQuotation :execrows
UPDATE cost_scenarios SET customer_quotation_id=$3,customer_quote_no=$4,updated_at=now()
WHERE tenant_id=$1 AND id=$2 AND status='CONFIRMED' AND customer_quotation_id IS NULL;

-- name: MarkSourcingCaseQuoted :exec
UPDATE sourcing_cases SET status='CUSTOMER_QUOTE_CREATED',updated_at=now() WHERE tenant_id=$1 AND id=$2;

-- name: AcceptedQuotationScenario :one
SELECT id, case_id, scenario_no, currency
FROM cost_scenarios
WHERE tenant_id=sqlc.arg(tenant_id) AND customer_quotation_id=sqlc.arg(quotation_id)
  AND status='CONFIRMED';

-- name: AcceptedQuotationLines :many
SELECT cl.sourcing_line_id, cl.supplier_quote_line_id,
       r.supplier_id, r.supplier_code, r.supplier_name,
       r.factory_id, r.factory_code, r.factory_name,
       cl.product_id, coalesce(cl.sku_id,0)::bigint AS sku_id,
       cl.product_name, cl.spec_snapshot, cl.qty::text AS qty, cl.uom_code,
       cl.source_currency, cl.source_unit_price::text AS source_unit_price,
       coalesce(ql.moq::text,'')::text AS moq, ql.lead_time
FROM cost_scenario_lines cl
JOIN supplier_quote_lines ql ON ql.id=cl.supplier_quote_line_id AND ql.tenant_id=cl.tenant_id
JOIN supplier_quotes sq ON sq.id=ql.supplier_quote_id AND sq.tenant_id=ql.tenant_id
JOIN factory_rfqs r ON r.id=sq.factory_rfq_id AND r.tenant_id=sq.tenant_id
WHERE cl.tenant_id=sqlc.arg(tenant_id) AND cl.scenario_id=sqlc.arg(scenario_id)
ORDER BY r.supplier_id, cl.sourcing_line_id;
