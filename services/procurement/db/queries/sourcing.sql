-- name: CreateSourcingCase :one
INSERT INTO sourcing_cases (
    tenant_id, case_no, title, customer_id, customer_name, contact_name,
    contact_email, source_mail_id, source_attachment_id, owner_id, owner_name
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    'SC-' || to_char(current_date, 'YYYYMMDD') || '-' || lpad(nextval('sourcing_case_no_seq')::text, 6, '0'),
    sqlc.arg(title)::text, sqlc.arg(customer_id)::bigint,
    sqlc.arg(customer_name)::text, sqlc.arg(contact_name)::text,
    sqlc.arg(contact_email)::text, sqlc.arg(source_mail_id)::bigint,
    sqlc.arg(source_attachment_id)::bigint, sqlc.arg(owner_id)::bigint,
    sqlc.arg(owner_name)::text
)
RETURNING id, case_no;

-- name: CreateSourcingLine :exec
INSERT INTO sourcing_lines (
    tenant_id, case_id, line_no, raw_text, product, material_standard, grade,
    thickness, width, length_or_form, surface_requirement, coating, tolerance,
    coil_weight, coil_id, packaging, delivery, payment_terms, incoterm, port,
    quantity_unit, remarks, quantity
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(case_id)::bigint, sqlc.arg(line_no)::int,
    sqlc.arg(raw_text)::text, sqlc.arg(product)::text,
    sqlc.arg(material_standard)::text, sqlc.arg(grade)::text,
    sqlc.arg(thickness)::text, sqlc.arg(width)::text, sqlc.arg(length_or_form)::text,
    sqlc.arg(surface_requirement)::text, sqlc.arg(coating)::text,
    sqlc.arg(tolerance)::text, sqlc.arg(coil_weight)::text, sqlc.arg(coil_id)::text,
    sqlc.arg(packaging)::text, sqlc.arg(delivery)::text,
    sqlc.arg(payment_terms)::text, sqlc.arg(incoterm)::text, sqlc.arg(port)::text,
    sqlc.arg(quantity_unit)::text, sqlc.arg(remarks)::text,
    nullif(sqlc.arg(quantity)::text, '')::numeric
);

-- name: ListSourcingCases :many
SELECT id, case_no, title, customer_id, customer_name, contact_name,
       contact_email, source_mail_id, source_attachment_id, status,
       owner_id, owner_name, created_at, updated_at, count(*) OVER () AS total
FROM sourcing_cases
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
  AND (sqlc.arg(keyword)::text = '' OR case_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR title ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR customer_name ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY updated_at DESC, id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: GetSourcingCase :one
SELECT id, case_no, title, customer_id, customer_name, contact_name,
       contact_email, source_mail_id, source_attachment_id, status,
       owner_id, owner_name, created_at, updated_at
FROM sourcing_cases
WHERE tenant_id = $1 AND id = $2;

-- name: ListSourcingLines :many
SELECT id, case_id, line_no, raw_text, product, material_standard, grade,
       thickness, width, length_or_form, surface_requirement, coating, tolerance,
       coil_weight, coil_id, packaging, delivery, payment_terms, incoterm, port,
       quantity_unit, remarks, coalesce(quantity::text, '')::text AS quantity,
       product_id, sku_id, uom_id, decision
FROM sourcing_lines
WHERE tenant_id = $1 AND case_id = $2
ORDER BY line_no;
