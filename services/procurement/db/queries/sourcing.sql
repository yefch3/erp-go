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
  AND (sqlc.arg(visible_all)::bool
       OR owner_id = ANY(sqlc.arg(visible_ids)::bigint[]))
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
       product_id, sku_id, uom_id, decision, decided_by, decided_by_name,
       coalesce(decided_at::text, '')::text AS decided_at
FROM sourcing_lines
WHERE tenant_id = $1 AND case_id = $2
ORDER BY line_no;

-- name: ReviewSourcingLine :execrows
UPDATE sourcing_lines SET
 product=sqlc.arg(product),material_standard=sqlc.arg(material_standard),grade=sqlc.arg(grade),
 thickness=sqlc.arg(thickness),width=sqlc.arg(width),length_or_form=sqlc.arg(length_or_form),
 surface_requirement=sqlc.arg(surface_requirement),coating=sqlc.arg(coating),tolerance=sqlc.arg(tolerance),
 coil_weight=sqlc.arg(coil_weight),coil_id=sqlc.arg(coil_id),packaging=sqlc.arg(packaging),
 delivery=sqlc.arg(delivery),payment_terms=sqlc.arg(payment_terms),incoterm=sqlc.arg(incoterm),
 port=sqlc.arg(port),quantity_unit=sqlc.arg(quantity_unit),remarks=sqlc.arg(remarks),
 quantity=nullif(sqlc.arg(quantity)::text,'')::numeric,product_id=sqlc.arg(product_id),
 sku_id=sqlc.arg(sku_id),uom_id=sqlc.arg(uom_id),decision=sqlc.arg(decision),
 decided_by=sqlc.arg(decided_by),decided_by_name=sqlc.arg(decided_by_name),
 decided_at=CASE WHEN sqlc.arg(decision)::varchar='PENDING' THEN NULL ELSE now() END,updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND case_id=sqlc.arg(case_id) AND id=sqlc.arg(id);

-- name: ConfirmSourcingLines :execrows
UPDATE sourcing_lines SET decision='CONFIRMED',updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND case_id=sqlc.arg(case_id)
  AND id = ANY(sqlc.arg(ids)::bigint[]);
