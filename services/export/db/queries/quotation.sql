-- Money and rates cross this boundary as text: Go holds decimal strings and
-- never a float64. See pkg/money.

-- name: CreateQuotation :one
INSERT INTO quotations (
    tenant_id, quote_no, customer_id, customer_name,
    contact_id, contact_name, contact_email, currency, incoterm,
    port_of_loading, port_of_discharge, payment_method, valid_until,
    fx_rate, fx_rate_at, fx_source, fx_base_currency,
    total_amount, base_amount, remark, sales_employee_id, sales_employee,
    created_by, updated_by, source_cost_scenario_id, source_cost_scenario_no, source_sourcing_case_id,
    source_customer_selection_id, source_customer_selection_no, source_customer_selection_version
) VALUES (
    $1, $2, $3, $4,
    nullif(sqlc.arg(contact_id)::bigint, 0), sqlc.arg(contact_name)::text, sqlc.arg(contact_email)::text,
    $5, $6, $7, $8, $9,
    nullif(sqlc.arg(valid_until)::text, '')::date,
    sqlc.arg(fx_rate)::text::numeric, sqlc.arg(fx_rate_at)::timestamptz,
    $10, $11,
    sqlc.arg(total_amount)::text::numeric, sqlc.arg(base_amount)::text::numeric,
    $12, $13, $14, $15, $15,
    nullif(sqlc.arg(source_cost_scenario_id)::bigint, 0), sqlc.arg(source_cost_scenario_no)::text,
    nullif(sqlc.arg(source_sourcing_case_id)::bigint, 0),
    nullif(sqlc.arg(source_customer_selection_id)::bigint, 0), sqlc.arg(source_customer_selection_no)::text,
    sqlc.arg(source_customer_selection_version)::int
)
RETURNING id;

-- name: UpdateQuotationHeader :execrows
UPDATE quotations SET
    customer_id = $3, customer_name = $4,
    contact_id = nullif(sqlc.arg(contact_id)::bigint, 0),
    contact_name = sqlc.arg(contact_name)::text,
    contact_email = sqlc.arg(contact_email)::text,
    currency = $5, incoterm = $6,
    port_of_loading = $7, port_of_discharge = $8, payment_method = $9,
    valid_until = nullif(sqlc.arg(valid_until)::text, '')::date,
    total_amount = sqlc.arg(total_amount)::text::numeric,
    base_amount = sqlc.arg(base_amount)::text::numeric,
    remark = $10, updated_at = now(), updated_by = $11
WHERE tenant_id = $1 AND id = $2 AND status = 'DRAFT';

-- name: GetQuotation :one
SELECT
    id, tenant_id, quote_no, customer_id, customer_name,
    coalesce(contact_id, 0)::bigint AS contact_id, contact_name, contact_email,
    currency, incoterm,
    port_of_loading, port_of_discharge, payment_method,
    coalesce(valid_until::text, '')::text AS valid_until,
    fx_rate::text AS fx_rate, fx_rate_at, fx_source, fx_base_currency,
    total_amount::text AS total_amount, base_amount::text AS base_amount,
    remark, status, respond_note, sales_employee_id, sales_employee, sent_at, responded_at, created_at,
    coalesce(source_cost_scenario_id, 0)::bigint AS source_cost_scenario_id,
    source_cost_scenario_no, coalesce(source_sourcing_case_id, 0)::bigint AS source_sourcing_case_id,
    coalesce(source_customer_selection_id, 0)::bigint AS source_customer_selection_id,
    source_customer_selection_no, source_customer_selection_version
FROM quotations
WHERE tenant_id = $1 AND id = $2;

-- name: GetQuotationIDByCustomerSelection :one
SELECT id FROM quotations
WHERE tenant_id=sqlc.arg(tenant_id) AND source_customer_selection_id=sqlc.arg(source_customer_selection_id)
  AND status <> 'CANCELLED';

-- name: GetQuotationIDByCostScenario :one
SELECT id
FROM quotations
WHERE tenant_id = sqlc.arg(tenant_id)
  AND source_cost_scenario_id = sqlc.arg(source_cost_scenario_id);

-- name: ListQuotations :many
SELECT
    q.id, q.quote_no, q.customer_id, q.customer_name, q.currency,
    q.total_amount::text AS total_amount, q.base_amount::text AS base_amount,
    q.status, q.respond_note, q.sales_employee_id, q.sales_employee, coalesce(q.valid_until::text, '')::text AS valid_until,
    q.created_at, coalesce(q.source_cost_scenario_id, 0)::bigint AS source_cost_scenario_id,
    q.source_cost_scenario_no, coalesce(q.source_sourcing_case_id, 0)::bigint AS source_sourcing_case_id,
    coalesce(q.source_customer_selection_id, 0)::bigint AS source_customer_selection_id,
    q.source_customer_selection_no, q.source_customer_selection_version,
    count(*) OVER () AS total
-- Aliased because the correlated subquery below brings a second table into
-- scope, and an unqualified tenant_id would then be ambiguous.
FROM quotations q
WHERE q.tenant_id = sqlc.arg(tenant_id)::bigint
  -- Same data scope as contracts; see iam.VisibleEmployees.
  AND (sqlc.arg(visible_all)::bool
       OR q.sales_employee_id = ANY(sqlc.arg(visible_ids)::bigint[]))
  AND (sqlc.arg(status)::text = '' OR q.status = sqlc.arg(status)::text)
  AND (sqlc.arg(customer_id)::bigint = 0 OR q.customer_id = sqlc.arg(customer_id)::bigint)
  AND (sqlc.arg(source_sourcing_case_id)::bigint = 0
       OR q.source_sourcing_case_id = sqlc.arg(source_sourcing_case_id)::bigint)
  AND (sqlc.arg(keyword)::text = ''
       OR q.quote_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR q.customer_name ILIKE '%' || sqlc.arg(keyword)::text || '%')
  -- For the "generate a contract" picker: offering an offer that already has
  -- a contract would only produce a guaranteed error.
  AND (sqlc.arg(without_contract)::bool = false
       OR NOT EXISTS (SELECT 1 FROM contracts c
                      WHERE c.tenant_id = q.tenant_id
                        AND c.quotation_id = q.id
                        AND c.status <> 'CANCELLED'))
ORDER BY q.id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: SetQuotationStatus :one
UPDATE quotations SET
    status = sqlc.arg(new_status)::text,
    sent_at = CASE WHEN sqlc.arg(new_status)::text = 'SENT' THEN now() ELSE sent_at END,
    responded_at = CASE WHEN sqlc.arg(new_status)::text IN ('ACCEPTED','REJECTED')
                        THEN now() ELSE responded_at END,
    -- The customer's words land with the answer and only with the answer.
    respond_note = CASE WHEN sqlc.arg(new_status)::text IN ('ACCEPTED','REJECTED')
                        THEN sqlc.arg(respond_note)::text ELSE respond_note END,
    updated_at = now(), updated_by = $3
WHERE tenant_id = $1 AND id = $2
RETURNING status;

-- name: DeleteQuotationItems :exec
DELETE FROM quotation_items WHERE tenant_id = $1 AND quotation_id = $2;

-- name: AddQuotationItem :exec
INSERT INTO quotation_items (
    tenant_id, quotation_id, line_no, product_id, sku_id, product_code,
    product_name, spec, qty, uom_id, uom_code, unit_price, amount, remark,
    source_cost_scenario_line_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    sqlc.arg(qty)::text::numeric, $9, $10,
    sqlc.arg(unit_price)::text::numeric, sqlc.arg(amount)::text::numeric, $11,
    nullif(sqlc.arg(source_cost_scenario_line_id)::bigint, 0)
);

-- name: ListQuotationItems :many
SELECT
    id, quotation_id, line_no, product_id, sku_id, product_code, product_name,
    spec, qty::text AS qty, uom_id, uom_code,
    unit_price::text AS unit_price, amount::text AS amount, remark,
    coalesce(source_cost_scenario_line_id, 0)::bigint AS source_cost_scenario_line_id
FROM quotation_items
WHERE tenant_id = $1 AND quotation_id = $2
ORDER BY line_no;

-- name: AddQuotationShipment :exec
INSERT INTO quotation_shipments (
    tenant_id, quotation_id, batch_no, source_customer_selection_shipment_id,
    shipment_group_key, carrier_forwarder, service_option_name, customer_managed,
    currency, freight_amount, charge_basis, port_of_loading, port_of_discharge,
    estimated_departure, estimated_arrival, valid_until, remark
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(quotation_id), sqlc.arg(batch_no),
    nullif(sqlc.arg(source_customer_selection_shipment_id)::bigint, 0),
    sqlc.arg(shipment_group_key), sqlc.arg(carrier_forwarder), sqlc.arg(service_option_name),
    sqlc.arg(customer_managed), sqlc.arg(currency), sqlc.arg(freight_amount)::text::numeric,
    sqlc.arg(charge_basis), sqlc.arg(port_of_loading), sqlc.arg(port_of_discharge),
    nullif(sqlc.arg(estimated_departure)::text, '')::date,
    nullif(sqlc.arg(estimated_arrival)::text, '')::date,
    nullif(sqlc.arg(valid_until)::text, '')::date, sqlc.arg(remark)
);

-- name: ListQuotationShipments :many
SELECT id, quotation_id, batch_no,
    coalesce(source_customer_selection_shipment_id, 0)::bigint AS source_customer_selection_shipment_id,
    shipment_group_key, carrier_forwarder, service_option_name, customer_managed,
    currency, freight_amount::text AS freight_amount, charge_basis,
    port_of_loading, port_of_discharge,
    coalesce(estimated_departure::text, '')::text AS estimated_departure,
    coalesce(estimated_arrival::text, '')::text AS estimated_arrival,
    coalesce(valid_until::text, '')::text AS valid_until, remark
FROM quotation_shipments
WHERE tenant_id = sqlc.arg(tenant_id) AND quotation_id = sqlc.arg(quotation_id)
ORDER BY batch_no;

-- name: SetQuotationOwner :execrows
UPDATE quotations SET
    sales_employee_id = sqlc.arg(owner_id),
    sales_employee    = sqlc.arg(owner_name),
    updated_by        = sqlc.arg(updated_by),
    updated_at        = now()
WHERE tenant_id = $1 AND id = sqlc.arg(id);
