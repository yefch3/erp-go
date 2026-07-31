-- name: UpsertAttributeTemplate :one
INSERT INTO attribute_templates (tenant_id, category_id, name)
VALUES (sqlc.arg(tenant_id)::bigint, sqlc.arg(category_id)::bigint, sqlc.arg(name)::text)
ON CONFLICT (tenant_id, category_id) DO UPDATE SET name = EXCLUDED.name
RETURNING id;

-- name: GetTemplateByCategory :one
SELECT id, category_id, name FROM attribute_templates
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND category_id = sqlc.arg(category_id)::bigint;

-- name: DeleteDefsOfTemplate :exec
DELETE FROM attribute_defs
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND template_id = sqlc.arg(template_id)::bigint;

-- name: AddAttributeDef :exec
INSERT INTO attribute_defs (
    tenant_id, template_id, key, label, data_type, unit, enum_values,
    match_strategy, tolerance_pct, level, is_matchable, is_required, sort_order
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(template_id)::bigint,
    sqlc.arg(key)::text,
    sqlc.arg(label)::text,
    sqlc.arg(data_type)::text,
    sqlc.arg(unit)::text,
    sqlc.arg(enum_values)::jsonb,
    sqlc.arg(match_strategy)::text,
    nullif(sqlc.arg(tolerance_pct)::text, '')::numeric,
    sqlc.arg(level)::text,
    sqlc.arg(is_matchable)::bool,
    sqlc.arg(is_required)::bool,
    sqlc.arg(sort_order)::int
);

-- name: ListDefsOfTemplate :many
SELECT
    id, template_id, key, label, data_type, unit, enum_values,
    match_strategy, coalesce(tolerance_pct::text, '')::text AS tolerance_pct,
    level, is_matchable, is_required, sort_order
FROM attribute_defs
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND template_id = sqlc.arg(template_id)::bigint
ORDER BY sort_order, id;

-- name: DefsForLineage :many
-- Resolve a category's attributes along its ancestry. `category_ids` arrives
-- root-first, so `array_position` orders the rows shallow → deep and the
-- caller merges by key with the deeper definition winning: a child category
-- may add attributes or tighten a tolerance without restating the parent.
SELECT
    d.id, t.category_id, d.key, d.label, d.data_type, d.unit, d.enum_values,
    d.match_strategy, coalesce(d.tolerance_pct::text, '')::text AS tolerance_pct,
    d.level, d.is_matchable, d.is_required, d.sort_order
FROM attribute_defs d
JOIN attribute_templates t ON t.id = d.template_id
WHERE d.tenant_id = sqlc.arg(tenant_id)::bigint
  AND t.category_id = ANY(sqlc.arg(category_ids)::bigint[])
ORDER BY array_position(sqlc.arg(category_ids)::bigint[], t.category_id), d.sort_order, d.id;

-- name: SetProductAttributes :execrows
UPDATE products
SET attributes = sqlc.arg(attributes)::jsonb, updated_at = now(), updated_by = sqlc.arg(updated_by)::bigint
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: SetSkuAttributes :execrows
UPDATE skus
SET attributes = sqlc.arg(attributes)::jsonb,
    spec = CASE WHEN sqlc.arg(spec)::text = '' THEN spec ELSE sqlc.arg(spec)::text END,
    attr_signature = sqlc.arg(attr_signature)::text
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: GetSku :one
SELECT id, product_id, code, spec, attributes, attr_signature, status
FROM skus
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: SearchSkusByAttributes :many
-- Structured recall. `product_exact` and `sku_exact` are JSONB containment
-- filters; `ranges` is an array of {key,min,max} applied to SKU attributes.
--
-- The double negative reads as "for every range, this SKU satisfies it":
-- there must be no range that the row fails. `jsonb_typeof` guards the cast,
-- because a key holding text would otherwise abort the whole query rather
-- than exclude the row.
SELECT
    p.id AS product_id, p.code AS product_code, p.name AS product_name,
    p.attributes AS product_attributes,
    s.id AS sku_id, s.code AS sku_code, s.spec, s.attributes AS sku_attributes,
    count(*) OVER () AS total
FROM skus s
JOIN products p ON p.id = s.product_id
WHERE s.tenant_id = sqlc.arg(tenant_id)::bigint
  AND s.status = 'ACTIVE' AND p.status = 'ACTIVE'
  AND (sqlc.arg(category_id)::bigint = 0 OR p.category_id = sqlc.arg(category_id)::bigint)
  AND p.attributes @> sqlc.arg(product_exact)::jsonb
  AND s.attributes @> sqlc.arg(sku_exact)::jsonb
  AND NOT EXISTS (
      SELECT 1 FROM jsonb_array_elements(sqlc.arg(ranges)::jsonb) r
      WHERE NOT (
          jsonb_typeof(s.attributes -> (r->>'key')) = 'number'
          AND (s.attributes ->> (r->>'key'))::numeric
              BETWEEN (r->>'min')::numeric AND (r->>'max')::numeric
      )
  )
ORDER BY p.id, s.id
LIMIT sqlc.arg(row_limit)::int;

-- name: SearchSkusByText :many
-- The level-0 path: no template, no structured values, still has to return
-- something. Trigram similarity over the product name and the free-text spec.
SELECT
    p.id AS product_id, p.code AS product_code, p.name AS product_name,
    s.id AS sku_id, s.code AS sku_code, s.spec,
    greatest(
        similarity(p.name, sqlc.arg(keyword)::text),
        similarity(s.spec, sqlc.arg(keyword)::text)
    )::text AS score
FROM skus s
JOIN products p ON p.id = s.product_id
WHERE s.tenant_id = sqlc.arg(tenant_id)::bigint
  AND s.status = 'ACTIVE' AND p.status = 'ACTIVE'
  AND (p.name % sqlc.arg(keyword)::text OR s.spec % sqlc.arg(keyword)::text)
ORDER BY score DESC
LIMIT sqlc.arg(row_limit)::int;
