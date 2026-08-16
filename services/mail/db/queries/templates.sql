-- name: ListEmailTemplates :many
-- Both layers at once, same shape as ListSignatures: the company phrasebook
-- and this person's own. The caller decides how to present them; the query
-- does not hide either.
SELECT id, owner_type, owner_id, name, lang, subject, content, body_format
FROM email_templates
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND (owner_type = 'TENANT' OR owner_id = sqlc.arg(employee_id)::bigint)
ORDER BY owner_type, name, lang, id;

-- name: CreateEmailTemplate :one
INSERT INTO email_templates
    (tenant_id, owner_type, owner_id, name, lang, subject, content, body_format)
VALUES
    (sqlc.arg(tenant_id)::bigint, sqlc.arg(owner_type)::text,
     sqlc.arg(owner_id)::bigint, sqlc.arg(name)::text, sqlc.arg(lang)::text,
     sqlc.arg(subject)::text, sqlc.arg(content)::text,
     sqlc.arg(body_format)::text)
RETURNING id;

-- name: UpdateEmailTemplate :execrows
-- The owner guard is on the WHERE, exactly as for signatures: you may edit
-- the company phrasebook or your own templates, and nobody else's. A row the
-- caller may not touch simply matches nothing.
UPDATE email_templates
SET owner_type  = sqlc.arg(owner_type)::text,
    owner_id    = sqlc.arg(owner_id)::bigint,
    name        = sqlc.arg(name)::text,
    lang        = sqlc.arg(lang)::text,
    subject     = sqlc.arg(subject)::text,
    content     = sqlc.arg(content)::text,
    body_format = sqlc.arg(body_format)::text,
    updated_at  = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND (owner_type = 'TENANT' OR owner_id = sqlc.arg(employee_id)::bigint);

-- name: DeleteEmailTemplate :execrows
-- Same guard as the update, for the same reason the signature delete grew
-- one: without it any colleague holding mail:email:write could remove the
-- phrases somebody sells with.
DELETE FROM email_templates
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND (owner_type = 'TENANT' OR owner_id = sqlc.arg(employee_id)::bigint);

-- name: GetEmailTemplate :one
SELECT id, owner_type, owner_id, name, lang, subject, content, body_format
FROM email_templates
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;
