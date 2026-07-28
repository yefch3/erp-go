-- Money and rates cross this boundary as text: Go holds decimal strings and
-- never a float64. See pkg/money.

-- name: CreateQuotation :one
INSERT INTO quotations (
    tenant_id, quote_no, customer_id, customer_name, currency, incoterm,
    port_of_loading, port_of_discharge, payment_method, valid_until,
    fx_rate, fx_rate_at, fx_source, fx_base_currency,
    total_amount, base_amount, remark, sales_employee_id, sales_employee,
    created_by, updated_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9,
    nullif(sqlc.arg(valid_until)::text, '')::date,
    sqlc.arg(fx_rate)::text::numeric, sqlc.arg(fx_rate_at)::timestamptz,
    $10, $11,
    sqlc.arg(total_amount)::text::numeric, sqlc.arg(base_amount)::text::numeric,
    $12, $13, $14, $15, $15
)
RETURNING id;

-- name: UpdateQuotationHeader :execrows
UPDATE quotations SET
    customer_id = $3, customer_name = $4, currency = $5, incoterm = $6,
    port_of_loading = $7, port_of_discharge = $8, payment_method = $9,
    valid_until = nullif(sqlc.arg(valid_until)::text, '')::date,
    total_amount = sqlc.arg(total_amount)::text::numeric,
    base_amount = sqlc.arg(base_amount)::text::numeric,
    remark = $10, updated_at = now(), updated_by = $11
WHERE tenant_id = $1 AND id = $2 AND status = 'DRAFT';

-- name: GetQuotation :one
SELECT
    id, tenant_id, quote_no, customer_id, customer_name, currency, incoterm,
    port_of_loading, port_of_discharge, payment_method,
    coalesce(valid_until::text, '')::text AS valid_until,
    fx_rate::text AS fx_rate, fx_rate_at, fx_source, fx_base_currency,
    total_amount::text AS total_amount, base_amount::text AS base_amount,
    remark, status, sales_employee_id, sales_employee, sent_at, responded_at, created_at
FROM quotations
WHERE tenant_id = $1 AND id = $2;

-- name: ListQuotations :many
SELECT
    id, quote_no, customer_id, customer_name, currency,
    total_amount::text AS total_amount, base_amount::text AS base_amount,
    status, sales_employee, coalesce(valid_until::text, '')::text AS valid_until,
    created_at, count(*) OVER () AS total
FROM quotations
WHERE tenant_id = $1
  AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
  AND (sqlc.arg(customer_id)::bigint = 0 OR customer_id = sqlc.arg(customer_id)::bigint)
  AND (sqlc.arg(keyword)::text = ''
       OR quote_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR customer_name ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: SetQuotationStatus :one
UPDATE quotations SET
    status = sqlc.arg(new_status)::text,
    sent_at = CASE WHEN sqlc.arg(new_status)::text = 'SENT' THEN now() ELSE sent_at END,
    responded_at = CASE WHEN sqlc.arg(new_status)::text IN ('ACCEPTED','REJECTED')
                        THEN now() ELSE responded_at END,
    updated_at = now(), updated_by = $3
WHERE tenant_id = $1 AND id = $2
RETURNING status;

-- name: DeleteQuotationItems :exec
DELETE FROM quotation_items WHERE tenant_id = $1 AND quotation_id = $2;

-- name: AddQuotationItem :exec
INSERT INTO quotation_items (
    tenant_id, quotation_id, line_no, product_id, sku_id, product_code,
    product_name, spec, qty, uom_id, uom_code, unit_price, amount, remark
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    sqlc.arg(qty)::text::numeric, $9, $10,
    sqlc.arg(unit_price)::text::numeric, sqlc.arg(amount)::text::numeric, $11
);

-- name: ListQuotationItems :many
SELECT
    id, quotation_id, line_no, product_id, sku_id, product_code, product_name,
    spec, qty::text AS qty, uom_id, uom_code,
    unit_price::text AS unit_price, amount::text AS amount, remark
FROM quotation_items
WHERE tenant_id = $1 AND quotation_id = $2
ORDER BY line_no;
