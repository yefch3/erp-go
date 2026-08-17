-- name: ImportRequirementCandidates :many
SELECT id, contract_no, product_name, product_code, spec, uom_code,
       required_qty::text AS required_qty,
       ordered_qty::text AS ordered_qty,
       (required_qty - ordered_qty)::text AS open_qty,
       status
FROM purchase_requirements
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND status IN ('PENDING', 'PARTIALLY_ORDERED')
  AND (product_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR product_code ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR spec ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY required_date NULLS LAST, id
LIMIT 50;

-- name: CreatePurchaseOrderImport :one
INSERT INTO purchase_order_imports (
    tenant_id, import_token, source_type, source_mail_id,
    source_attachment_id, source_file_name, file_sha256, preview_rows,
    created_by, expires_at
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(import_token), sqlc.arg(source_type),
    sqlc.arg(source_mail_id), sqlc.arg(source_attachment_id),
    sqlc.arg(source_file_name), sqlc.arg(file_sha256),
    sqlc.arg(preview_rows), sqlc.arg(created_by), sqlc.arg(expires_at)
)
RETURNING import_token, expires_at;

-- name: LockPurchaseOrderImport :one
SELECT id, import_token, source_type, source_mail_id, source_attachment_id,
       source_file_name, file_sha256, preview_rows, status,
       coalesce(purchase_order_id, 0)::bigint AS purchase_order_id,
       created_by, expires_at
FROM purchase_order_imports
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND import_token = sqlc.arg(import_token)::text
FOR UPDATE;

-- name: MarkPurchaseOrderImportCreated :exec
UPDATE purchase_order_imports
SET status = 'CREATED', purchase_order_id = sqlc.arg(purchase_order_id),
    confirmed_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND status = 'PREVIEWED';

-- name: MarkPurchaseOrderImportExpired :exec
UPDATE purchase_order_imports
SET status = 'EXPIRED'
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND status = 'PREVIEWED';

-- name: PurchaseOrderImportResult :one
SELECT id, po_no, status
FROM purchase_orders
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint;
