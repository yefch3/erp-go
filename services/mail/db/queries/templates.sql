-- name: ListEmailTemplates :many
-- Only this person's own, same shape as ListSignatures: a template is its
-- writer's data (2026-09-18), the company phrasebook layer is gone.
SELECT id, owner_id, name, lang, subject, content, body_format
FROM email_templates
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND owner_id = sqlc.arg(employee_id)::bigint
ORDER BY name, lang, id;

-- name: CreateEmailTemplate :one
-- owner_type is left to its default: the column outlives the concept by one
-- release (see migration 00070) and nothing reads it any more.
INSERT INTO email_templates
    (tenant_id, owner_id, name, lang, subject, content, body_format)
VALUES
    (sqlc.arg(tenant_id)::bigint, sqlc.arg(owner_id)::bigint,
     sqlc.arg(name)::text, sqlc.arg(lang)::text,
     sqlc.arg(subject)::text, sqlc.arg(content)::text,
     sqlc.arg(body_format)::text)
RETURNING id;

-- name: UpdateEmailTemplate :execrows
-- The owner guard is on the WHERE, exactly as for signatures: you may edit
-- your own templates and nobody else's. A row the caller may not touch
-- simply matches nothing.
UPDATE email_templates
SET name        = sqlc.arg(name)::text,
    lang        = sqlc.arg(lang)::text,
    subject     = sqlc.arg(subject)::text,
    content     = sqlc.arg(content)::text,
    body_format = sqlc.arg(body_format)::text,
    updated_at  = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND owner_id = sqlc.arg(employee_id)::bigint;

-- name: DeleteEmailTemplate :execrows
-- Same guard as the update, for the same reason the signature delete grew
-- one: without it any colleague holding mail:email:write could remove the
-- phrases somebody sells with.
DELETE FROM email_templates
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND owner_id = sqlc.arg(employee_id)::bigint;

-- name: GetEmailTemplate :one
-- Owner-scoped like the list, for the same reason GetSignature is.
SELECT id, owner_id, name, lang, subject, content, body_format
FROM email_templates
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND owner_id = sqlc.arg(employee_id)::bigint;
