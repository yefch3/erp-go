-- The one-time links that let somebody who cannot log in choose a new
-- password. Same query anatomy as the invitations above; the differences
-- (one-hour life, requested_by) are explained on the table.

-- name: DeleteLivePasswordResets :execrows
-- Re-requesting replaces the outstanding link rather than accumulating keys.
DELETE FROM password_resets
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND employee_id = sqlc.arg(employee_id)::bigint
  AND used_at IS NULL;

-- name: CreatePasswordReset :one
INSERT INTO password_resets (tenant_id, employee_id, email, token_hash, expires_at, requested_by)
VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(employee_id)::bigint,
    lower(sqlc.arg(email)::text),
    sqlc.arg(token_hash),
    sqlc.arg(expires_at),
    sqlc.arg(requested_by)::bigint
)
RETURNING id, expires_at;

-- name: GetPasswordResetByToken :one
-- Everything redemption needs in one round trip, expired and used rows
-- included — the page has to say WHY a link is dead, and "expired" and
-- "already used" are different sentences.
SELECT p.id, p.tenant_id, p.employee_id, p.email, p.expires_at, p.used_at,
       e.name AS employee_name, e.status AS employee_status,
       e.email AS current_email,
       t.status AS tenant_status
FROM password_resets p
JOIN employees e ON e.id = p.employee_id AND e.tenant_id = p.tenant_id
JOIN tenants t ON t.id = p.tenant_id
WHERE p.token_hash = sqlc.arg(token_hash);

-- name: ConsumePasswordReset :execrows
-- The WHERE clause is the concurrency control, same as ConsumeInvitation.
UPDATE password_resets
SET used_at = now()
WHERE id = sqlc.arg(id)::bigint AND used_at IS NULL;

-- name: SetMustChangePassword :execrows
-- Raised when an administrator typed the password (they know it; it must
-- open exactly one door), cleared the moment the person sets their own.
UPDATE users
SET must_change_password = sqlc.arg(must_change)::boolean, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND employee_id = sqlc.arg(employee_id)::bigint;

-- name: ClearLoginLock :exec
-- A lockout is a deadline for guessing the OLD password. Once the owner has
-- proven themselves through a reset link and chosen a new one, the deadline
-- has nothing left to wait for — leaving it would refuse the very person the
-- reset just rescued.
UPDATE users
SET failed_count = 0, locked_until = NULL, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND employee_id = sqlc.arg(employee_id)::bigint;

-- name: LatestInviterOf :one
-- Who brought this person into the system. The self-service reset mail needs
-- a sender with a bound mailbox, and the person's inviter is the one employee
-- guaranteed to have had one at some point.
SELECT invited_by FROM employee_invitations
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND employee_id = sqlc.arg(employee_id)::bigint
ORDER BY id DESC
LIMIT 1;
