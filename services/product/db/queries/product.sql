-- Numeric columns travel as text on purpose: Go keeps decimal strings so a
-- price never passes through float64. See pkg/money.

-- name: ListCategories :many
SELECT * FROM product_categories
WHERE tenant_id = $1 AND ($2::text = '' OR status = $2)
ORDER BY sort_order, code;

-- name: GetCategory :one
SELECT * FROM product_categories WHERE tenant_id = $1 AND id = $2;

-- name: CreateCategory :one
INSERT INTO product_categories (tenant_id, code, name, parent_id, path, level, sort_order)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateCategory :one
UPDATE product_categories SET name = $3, sort_order = $4
WHERE tenant_id = $1 AND id = $2
RETURNING *;

-- name: DeactivateCategory :execrows
UPDATE product_categories SET status = 'INACTIVE'
WHERE tenant_id = $1 AND id = $2 AND status = 'ACTIVE';

-- name: CountProductsInCategory :one
SELECT count(*) FROM products WHERE tenant_id = $1 AND category_id = $2;

-- name: ListUoms :many
SELECT * FROM uoms WHERE tenant_id = $1 AND status = 'ACTIVE' ORDER BY uom_type, code;

-- name: CreateUom :one
INSERT INTO uoms (tenant_id, code, name, uom_type) VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListProducts :many
SELECT
    p.id, p.tenant_id, p.code, p.name, p.name_en, p.category_id, p.product_type,
    p.brand, p.base_uom_id,
    -- coalesce, not a bare cast: NULL::text is still NULL, and sqlc infers a
    -- non-nullable string from the cast alone. A product saved without a
    -- reference price then breaks every read of it.
    coalesce(p.reference_price::text, '')::text     AS reference_price,
    p.reference_currency, p.hs_code,
    coalesce(p.tax_rate::text, '')::text            AS tax_rate,
    coalesce(p.export_rebate_rate::text, '')::text  AS export_rebate_rate,
    p.description, p.status, p.attributes,
    c.name AS category_name, u.code AS base_uom_code,
    count(*) OVER () AS total
FROM products p
JOIN product_categories c ON c.id = p.category_id AND c.tenant_id = p.tenant_id
JOIN uoms u ON u.id = p.base_uom_id AND u.tenant_id = p.tenant_id
WHERE p.tenant_id = $1
  AND (sqlc.arg(status)::text = 'ALL' OR p.status = coalesce(nullif(sqlc.arg(status)::text, ''), 'ACTIVE'))
  AND (sqlc.arg(category_id)::bigint = 0 OR p.category_id = sqlc.arg(category_id)::bigint)
  AND (sqlc.arg(keyword)::text = ''
       OR p.name ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR p.name_en ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR p.code ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY p.id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: GetProduct :one
SELECT
    p.id, p.tenant_id, p.code, p.name, p.name_en, p.category_id, p.product_type,
    p.brand, p.base_uom_id,
    -- coalesce, not a bare cast: NULL::text is still NULL, and sqlc infers a
    -- non-nullable string from the cast alone. A product saved without a
    -- reference price then breaks every read of it.
    coalesce(p.reference_price::text, '')::text     AS reference_price,
    p.reference_currency, p.hs_code,
    coalesce(p.tax_rate::text, '')::text            AS tax_rate,
    coalesce(p.export_rebate_rate::text, '')::text  AS export_rebate_rate,
    p.description, p.status, p.attributes,
    c.name AS category_name, u.code AS base_uom_code
FROM products p
JOIN product_categories c ON c.id = p.category_id AND c.tenant_id = p.tenant_id
JOIN uoms u ON u.id = p.base_uom_id AND u.tenant_id = p.tenant_id
WHERE p.tenant_id = $1 AND p.id = $2;

-- name: CreateProduct :one
INSERT INTO products (
    tenant_id, code, name, name_en, category_id, product_type, brand, base_uom_id,
    reference_price, reference_currency, hs_code, tax_rate, export_rebate_rate,
    description, created_by, updated_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    nullif(sqlc.arg(reference_price)::text, '')::numeric,
    $9, $10,
    nullif(sqlc.arg(tax_rate)::text, '')::numeric,
    nullif(sqlc.arg(export_rebate_rate)::text, '')::numeric,
    $11, $12, $12
)
RETURNING id;

-- name: UpdateProduct :execrows
UPDATE products SET
    name = $3, name_en = $4, category_id = $5, product_type = $6, brand = $7,
    base_uom_id = $8,
    reference_price = nullif(sqlc.arg(reference_price)::text, '')::numeric,
    reference_currency = $9, hs_code = $10,
    tax_rate = nullif(sqlc.arg(tax_rate)::text, '')::numeric,
    export_rebate_rate = nullif(sqlc.arg(export_rebate_rate)::text, '')::numeric,
    description = $11, updated_at = now(), updated_by = $12
WHERE tenant_id = $1 AND id = $2;

-- name: DeactivateProduct :execrows
UPDATE products SET status = 'INACTIVE', updated_at = now(), updated_by = $3
WHERE tenant_id = $1 AND id = $2 AND status <> 'INACTIVE';

-- name: ActivateProduct :execrows
UPDATE products SET status = 'ACTIVE', updated_at = now(), updated_by = $3
WHERE tenant_id = $1 AND id = $2 AND status = 'INACTIVE';

-- name: ListSkus :many
SELECT * FROM skus WHERE tenant_id = $1 AND product_id = $2 ORDER BY id;

-- name: CreateSku :one
INSERT INTO skus (tenant_id, product_id, code, spec, attributes, attr_signature)
VALUES ($1, $2, $3, $4, sqlc.arg(attributes)::jsonb, sqlc.arg(attr_signature)::text)
RETURNING *;

-- name: DeactivateSku :execrows
UPDATE skus SET status = 'INACTIVE'
WHERE tenant_id = $1 AND id = $2 AND status = 'ACTIVE';

-- name: ListAttachments :many
SELECT * FROM product_attachments
WHERE tenant_id = $1 AND product_id = $2
ORDER BY uploaded_at DESC;

-- name: GetAttachment :one
SELECT * FROM product_attachments WHERE tenant_id = $1 AND id = $2;

-- name: CreateAttachment :one
INSERT INTO product_attachments (
    tenant_id, product_id, file_name, file_key, file_size, content_type, uploaded_by
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: DeleteAttachment :execrows
DELETE FROM product_attachments WHERE tenant_id = $1 AND id = $2;
