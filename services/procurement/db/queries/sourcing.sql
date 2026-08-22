-- name: CreateSourcingCase :one
INSERT INTO sourcing_cases (
    tenant_id, case_no, title, customer_id, customer_name, contact_name,
    contact_email, source_mail_id, source_attachment_id, owner_id, owner_name,
    source_file_name, source_content_type, source_file_data,
    inquiry_template_id, inquiry_template_code, inquiry_template_version
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    'SC-' || to_char(current_date, 'YYYYMMDD') || '-' || lpad(nextval('sourcing_case_no_seq')::text, 6, '0'),
    sqlc.arg(title)::text, sqlc.arg(customer_id)::bigint,
    sqlc.arg(customer_name)::text, sqlc.arg(contact_name)::text,
    sqlc.arg(contact_email)::text, sqlc.arg(source_mail_id)::bigint,
    sqlc.arg(source_attachment_id)::bigint, sqlc.arg(owner_id)::bigint,
    sqlc.arg(owner_name)::text, sqlc.arg(source_file_name)::text,
    sqlc.arg(source_content_type)::text, sqlc.arg(source_file_data)::bytea,
    sqlc.arg(inquiry_template_id)::bigint, sqlc.arg(inquiry_template_code)::text,
    sqlc.arg(inquiry_template_version)::int
)
RETURNING id, case_no;

-- name: CreateSourcingLine :exec
INSERT INTO sourcing_lines (
    tenant_id, case_id, line_no, raw_text, product, material_standard, grade,
    thickness, width, length_or_form, surface_requirement, coating, tolerance,
    coil_weight, coil_id, packaging, delivery, payment_terms, incoterm, port,
    quantity_unit, remarks, quantity, custom_fields
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
    nullif(sqlc.arg(quantity)::text, '')::numeric,
    sqlc.arg(custom_fields)::jsonb
);

-- name: ListSourcingCases :many
SELECT id, case_no, title, customer_id, customer_name, contact_name,
       contact_email, source_mail_id, source_attachment_id, status,
       owner_id, owner_name, source_file_name, inquiry_template_id,
       inquiry_template_code, inquiry_template_version, created_at, updated_at,
       count(*) OVER () AS total
FROM sourcing_cases
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(visible_all)::bool
       OR owner_id = ANY(sqlc.arg(visible_ids)::bigint[]))
  -- 待复核询盘有独立页面；正式询价列表默认不混入尚未复核的数据。
  AND ((sqlc.arg(status)::text = '' AND status NOT IN ('INTAKE_PENDING','CANCELLED'))
       OR status = sqlc.arg(status)::text)
  AND (sqlc.arg(keyword)::text = '' OR case_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR title ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR customer_name ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY updated_at DESC, id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: GetSourcingCase :one
SELECT id, case_no, title, customer_id, customer_name, contact_name,
       contact_email, source_mail_id, source_attachment_id, status,
       owner_id, owner_name, source_file_name, inquiry_template_id,
       inquiry_template_code, inquiry_template_version, created_at, updated_at
FROM sourcing_cases
WHERE tenant_id = $1 AND id = $2;

-- name: ListSourcingLines :many
SELECT id, case_id, line_no, raw_text, product, material_standard, grade,
       thickness, width, length_or_form, surface_requirement, coating, tolerance,
       coil_weight, coil_id, packaging, delivery, payment_terms, incoterm, port,
       quantity_unit, remarks, coalesce(quantity::text, '')::text AS quantity,
       product_id, sku_id, uom_id, decision, decided_by, decided_by_name,
       coalesce(decided_at::text, '')::text AS decided_at, revision_no, custom_fields
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
 quantity=nullif(sqlc.arg(quantity)::text,'')::numeric,custom_fields=sqlc.arg(custom_fields)::jsonb,
 product_id=sqlc.arg(product_id),
 sku_id=sqlc.arg(sku_id),uom_id=sqlc.arg(uom_id),decision=sqlc.arg(decision),
 decided_by=sqlc.arg(decided_by),decided_by_name=sqlc.arg(decided_by_name),
 decided_at=CASE WHEN sqlc.arg(decision)::varchar='PENDING' THEN NULL ELSE now() END,
 revision_no=revision_no+CASE WHEN decision='CONFIRMED' THEN 1 ELSE 0 END,updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND case_id=sqlc.arg(case_id) AND id=sqlc.arg(id);

-- name: ConfirmSourcingLines :execrows
UPDATE sourcing_lines SET decision='CONFIRMED',updated_at=now()
WHERE tenant_id=sqlc.arg(tenant_id) AND case_id=sqlc.arg(case_id)
  AND id = ANY(sqlc.arg(ids)::bigint[]);

-- name: GetSourcingSourceFile :one
SELECT source_file_name, source_content_type, source_file_data
FROM sourcing_cases WHERE tenant_id=$1 AND id=$2;

-- name: CreateSourcingChange :exec
INSERT INTO sourcing_case_changes
 (tenant_id,case_id,section,action,entity_id,summary,before_json,after_json,reason,operator_id,operator_name)
VALUES (sqlc.arg(tenant_id),sqlc.arg(case_id),sqlc.arg(section),sqlc.arg(action),sqlc.arg(entity_id),
 sqlc.arg(summary),sqlc.arg(before_json)::jsonb,sqlc.arg(after_json)::jsonb,sqlc.arg(reason),
 sqlc.arg(operator_id),sqlc.arg(operator_name));

-- name: ListSourcingChanges :many
SELECT id,section,action,entity_id,summary,before_json,after_json,reason,
 operator_id,operator_name,created_at
FROM sourcing_case_changes
WHERE tenant_id=$1 AND case_id=$2
ORDER BY created_at DESC,id DESC;
