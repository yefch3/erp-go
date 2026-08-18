-- 标准询盘列模板。版本不可变：每次保存生成新版本，历史询盘继续引用旧版。
-- 唯一 ACTIVE（按编码）与唯一默认（按租户）由迁移 00012 的部分唯一索引保证。

-- name: ListInquiryTemplates :many
SELECT id, tenant_id, template_code, version, name, description, status,
       is_default, created_by, created_by_name, created_at, updated_at
FROM inquiry_templates
WHERE tenant_id = $1 AND status != 'SUPERSEDED'
ORDER BY is_default DESC, template_code, version DESC;

-- name: GetInquiryTemplate :one
SELECT id, tenant_id, template_code, version, name, description, status,
       is_default, created_by, created_by_name, created_at, updated_at
FROM inquiry_templates
WHERE tenant_id = $1 AND id = $2;

-- name: GetActiveInquiryTemplateByCode :one
SELECT id, tenant_id, template_code, version, name, description, status,
       is_default, created_by, created_by_name, created_at, updated_at
FROM inquiry_templates
WHERE tenant_id = $1 AND template_code = $2 AND status = 'ACTIVE';

-- name: GetDefaultInquiryTemplate :one
SELECT id, tenant_id, template_code, version, name, description, status,
       is_default, created_by, created_by_name, created_at, updated_at
FROM inquiry_templates
WHERE tenant_id = $1 AND status = 'ACTIVE' AND is_default;

-- name: MaxInquiryTemplateVersion :one
SELECT coalesce(max(version), 0)::int AS max_version
FROM inquiry_templates
WHERE tenant_id = $1 AND template_code = $2;

-- name: CreateInquiryTemplate :one
INSERT INTO inquiry_templates (
    tenant_id, template_code, version, name, description, status,
    is_default, created_by, created_by_name
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(template_code)::text,
    sqlc.arg(version)::int, sqlc.arg(name)::text, sqlc.arg(description)::text,
    sqlc.arg(status)::text, sqlc.arg(is_default)::bool,
    sqlc.arg(created_by)::bigint, sqlc.arg(created_by_name)::text
)
RETURNING id;

-- name: SupersedeActiveInquiryTemplate :execrows
UPDATE inquiry_templates SET status = 'SUPERSEDED', updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND template_code = sqlc.arg(template_code)::text AND status = 'ACTIVE';

-- name: SetInquiryTemplateStatus :execrows
UPDATE inquiry_templates SET status = sqlc.arg(status)::text, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
  AND status IN ('ACTIVE', 'DISABLED');

-- name: ClearDefaultInquiryTemplate :exec
UPDATE inquiry_templates SET is_default = false, updated_at = now()
WHERE tenant_id = $1 AND is_default;

-- name: SetDefaultInquiryTemplate :execrows
UPDATE inquiry_templates SET is_default = true, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND status = 'ACTIVE';

-- name: ListInquiryTemplateFields :many
SELECT id, template_id, field_key, display_name, sort_order, is_required,
       default_value, data_type, is_custom
FROM inquiry_template_fields
WHERE tenant_id = $1 AND template_id = $2
ORDER BY sort_order, id;

-- name: CreateInquiryTemplateField :exec
INSERT INTO inquiry_template_fields (
    tenant_id, template_id, field_key, display_name, sort_order, is_required,
    default_value, data_type, is_custom
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(template_id)::bigint,
    sqlc.arg(field_key)::text, sqlc.arg(display_name)::text,
    sqlc.arg(sort_order)::int, sqlc.arg(is_required)::bool,
    sqlc.arg(default_value)::text, sqlc.arg(data_type)::text,
    sqlc.arg(is_custom)::bool
);
