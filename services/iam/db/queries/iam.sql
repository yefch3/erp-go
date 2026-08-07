-- name: GetTenantByDomain :one
-- The login page has no idea which company somebody belongs to; the domain of
-- the address they type is what says so. A primary-key hit, because this runs
-- on every login attempt including every failed one.
SELECT d.tenant_id, t.name AS tenant_name, t.status AS tenant_status
FROM tenant_domains d
JOIN tenants t ON t.id = d.tenant_id
WHERE d.domain = lower(sqlc.arg(domain)::text);

-- name: GetUserByEmail :one
-- lower() on both sides: an address is case-insensitive in practice, and
-- "Alice@" must not be a second account from "alice@". email_verified_at rides
-- along because login has to refuse an account whose mailbox was never proved
-- to exist, and doing it in the same round trip keeps that check free.
SELECT u.id, u.tenant_id, u.employee_id, u.username, u.password_hash, u.status, u.failed_count,
       e.name AS employee_name, e.code AS employee_code, e.department_id,
       e.status AS employee_status, e.email_verified_at
FROM users u
JOIN employees e ON e.id = u.employee_id
WHERE u.tenant_id = sqlc.arg(tenant_id)::bigint
  AND e.email <> ''
  AND lower(e.email) = lower(sqlc.arg(email)::text);

-- name: GetUserByUsername :one
SELECT u.id, u.tenant_id, u.employee_id, u.username, u.password_hash, u.status, u.failed_count,
       e.name AS employee_name, e.code AS employee_code, e.department_id, e.status AS employee_status
FROM users u
JOIN employees e ON e.id = u.employee_id
WHERE u.tenant_id = $1 AND u.username = $2;

-- name: RecordLoginSuccess :exec
UPDATE users SET failed_count = 0, last_login_at = now(), updated_at = now() WHERE id = $1;

-- name: RecordLoginFailure :one
UPDATE users
SET failed_count = failed_count + 1,
    status = CASE WHEN failed_count + 1 >= 5 THEN 'LOCKED' ELSE status END,
    updated_at = now()
WHERE id = $1
RETURNING failed_count, status;

-- name: CreateDepartment :one
INSERT INTO departments (tenant_id, code, name, parent_id, path, level)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetDepartment :one
SELECT * FROM departments WHERE tenant_id = $1 AND id = $2;

-- name: ListDepartments :many
SELECT * FROM departments WHERE tenant_id = $1 ORDER BY path, sort_order, id;

-- name: CreateEmployee :one
INSERT INTO employees (tenant_id, code, name, department_id, position, email, phone, manager_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, nullif(sqlc.arg(manager_id)::bigint, 0))
RETURNING *;

-- name: SetEmployeeManager :execrows
UPDATE employees SET manager_id = nullif(sqlc.arg(manager_id)::bigint, 0), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: ManagerAtLevel :many
-- Walks the reporting line upwards: level 1 is the direct manager, level 2
-- their manager, and so on. Returns at most one row, and none once the chain
-- runs out - the person at the top reports to nobody, and a flow that climbs
-- past them simply has no one left to ask.
WITH RECURSIVE chain AS (
    SELECT e.id, e.manager_id, 0 AS lvl
    FROM employees e
    WHERE e.tenant_id = $1 AND e.id = $2
    UNION ALL
    SELECT m.id, m.manager_id, c.lvl + 1
    FROM chain c
    JOIN employees m ON m.id = c.manager_id AND m.status = 'ACTIVE'
    WHERE c.lvl < sqlc.arg(levels)::int
)
SELECT id FROM chain WHERE lvl = sqlc.arg(levels)::int;

-- name: GetEmployee :one
SELECT e.*, d.name AS department_name, coalesce(m.name, '')::text AS manager_name
FROM employees e
JOIN departments d ON d.id = e.department_id
LEFT JOIN employees m ON m.id = e.manager_id
WHERE e.tenant_id = $1 AND e.id = $2;

-- name: ListEmployees :many
SELECT e.*, d.name AS department_name, coalesce(m.name, '')::text AS manager_name,
       count(*) OVER () AS total
FROM employees e
JOIN departments d ON d.id = e.department_id
LEFT JOIN employees m ON m.id = e.manager_id
WHERE e.tenant_id = $1
  AND ($2::bigint = 0 OR e.department_id = $2)
  AND ($3::text = '' OR e.name ILIKE '%' || $3 || '%' OR e.code ILIKE '%' || $3 || '%')
ORDER BY e.id DESC
LIMIT $4 OFFSET $5;

-- name: DeactivateEmployee :execrows
UPDATE employees SET status = 'INACTIVE', updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND status = 'ACTIVE';

-- name: CreateUser :one
INSERT INTO users (tenant_id, employee_id, username, password_hash)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: CreateRole :one
INSERT INTO roles (tenant_id, code, name, description)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListRoles :many
SELECT * FROM roles WHERE tenant_id = $1 AND status = 'ACTIVE' ORDER BY id;

-- name: ListPermissions :many
SELECT * FROM permissions ORDER BY module, code;

-- name: GetPermissionIDsByCodes :many
SELECT id FROM permissions WHERE code = ANY($1::text[]);

-- name: ReplaceRolePermissions :exec
DELETE FROM role_permissions WHERE tenant_id = $1 AND role_id = $2;

-- name: AddRolePermission :exec
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
VALUES ($1, $2, $3) ON CONFLICT DO NOTHING;

-- name: ListRolePermissionCodes :many
SELECT p.code
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
WHERE rp.tenant_id = $1 AND rp.role_id = $2
ORDER BY p.code;

-- name: ReplaceEmployeeRoles :exec
DELETE FROM employee_roles WHERE tenant_id = $1 AND employee_id = $2;

-- name: AddEmployeeRole :exec
INSERT INTO employee_roles (tenant_id, employee_id, role_id)
VALUES ($1, $2, $3) ON CONFLICT DO NOTHING;

-- name: ListEmployeeRoleIDs :many
SELECT role_id FROM employee_roles WHERE tenant_id = $1 AND employee_id = $2 ORDER BY role_id;

-- name: ListEmployeePermissionCodes :many
SELECT DISTINCT p.code
FROM employee_roles er
JOIN employees e ON e.id = er.employee_id AND e.tenant_id = er.tenant_id
JOIN role_permissions rp ON rp.tenant_id = er.tenant_id AND rp.role_id = er.role_id
JOIN permissions p ON p.id = rp.permission_id
WHERE er.tenant_id = $1 AND er.employee_id = $2 AND e.status = 'ACTIVE'
ORDER BY p.code;

-- name: EmployeeHasPermission :one
-- The employee join is not decoration: without it a token issued before
-- someone left keeps working until it expires. Every guarded request runs
-- through here, so this is where "left the company" takes effect.
SELECT EXISTS (
    SELECT 1
    FROM employee_roles er
    JOIN employees e ON e.id = er.employee_id AND e.tenant_id = er.tenant_id
    JOIN role_permissions rp ON rp.tenant_id = er.tenant_id AND rp.role_id = er.role_id
    JOIN permissions p ON p.id = rp.permission_id
    WHERE er.tenant_id = $1 AND er.employee_id = $2 AND p.code = $3
      AND e.status = 'ACTIVE'
) AS allowed;

-- name: HasAnyUser :one
-- EXISTS stops at the first row: O(1) regardless of table size, unlike count(*).
SELECT EXISTS (SELECT 1 FROM users WHERE tenant_id = $1) AS has_users;

-- name: SetDepartmentPath :exec
UPDATE departments SET path = $3, level = $4 WHERE tenant_id = $1 AND id = $2;

-- name: ListRoleMembers :many
SELECT e.id AS employee_id, e.name
FROM employee_roles er
JOIN employees e ON e.id = er.employee_id AND e.tenant_id = er.tenant_id
WHERE er.tenant_id = $1 AND er.role_id = $2 AND e.status = 'ACTIVE'
ORDER BY e.id;

-- name: GetUserByEmployee :one
SELECT id, username, password_hash, status FROM users
WHERE tenant_id = $1 AND employee_id = $2;

-- name: UpdatePassword :execrows
UPDATE users SET password_hash = $3, failed_count = 0, updated_at = now()
WHERE tenant_id = $1 AND employee_id = $2;

-- name: ListEmployeeAccounts :many
-- Which employees can log in, for the employee list; a company usually has
-- more employees than accounts.
SELECT employee_id, username FROM users WHERE tenant_id = $1;

-- name: ActivateEmployee :execrows
UPDATE employees SET status = 'ACTIVE', updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND status = 'INACTIVE';

-- name: CountOtherHoldersOf :one
-- How many *other* active employees still hold a permission. Used to refuse
-- the deactivation that would leave nobody able to administer the system.
SELECT count(DISTINCT e.id) FROM employees e
JOIN employee_roles er ON er.employee_id = e.id AND er.tenant_id = e.tenant_id
JOIN role_permissions rp ON rp.role_id = er.role_id AND rp.tenant_id = er.tenant_id
JOIN permissions p ON p.id = rp.permission_id
WHERE e.tenant_id = $1 AND e.status = 'ACTIVE' AND e.id <> $2 AND p.code = $3;

-- name: WidestDataScope :one
-- Someone with several roles gets the widest of them: adding a role must
-- never take visibility away. Ordered by how much each scope reveals.
SELECT s.scope_type, s.custom_dept_ids
FROM role_data_scopes s
JOIN employee_roles er ON er.role_id = s.role_id AND er.tenant_id = s.tenant_id
WHERE s.tenant_id = $1 AND er.employee_id = $2 AND s.module = $3
ORDER BY CASE s.scope_type
           WHEN 'ALL' THEN 4
           WHEN 'CUSTOM' THEN 3
           WHEN 'DEPT_AND_SUB' THEN 2
           WHEN 'DEPT' THEN 1
           ELSE 0
         END DESC
LIMIT 1;

-- name: EmployeesInMyDept :many
SELECT peer.id
FROM employees me
JOIN employees peer ON peer.department_id = me.department_id AND peer.tenant_id = me.tenant_id
WHERE me.tenant_id = $1 AND me.id = $2 AND peer.status = 'ACTIVE';

-- name: EmployeesInMyDeptTree :many
-- The department path is materialised ("/1/4/"), so a subtree is a prefix
-- match rather than a recursive walk.
SELECT peer.id
FROM employees me
JOIN departments mine ON mine.id = me.department_id
JOIN departments sub ON sub.tenant_id = mine.tenant_id
                     AND (sub.id = mine.id OR sub.path LIKE mine.path || mine.id || '/%')
JOIN employees peer ON peer.department_id = sub.id AND peer.tenant_id = me.tenant_id
WHERE me.tenant_id = $1 AND me.id = $2 AND peer.status = 'ACTIVE';

-- name: EmployeesInDepts :many
SELECT id FROM employees
WHERE tenant_id = $1 AND department_id = ANY(sqlc.arg(dept_ids)::bigint[]) AND status = 'ACTIVE';

-- name: SetRoleDataScope :exec
INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type, custom_dept_ids)
VALUES ($1, $2, $3, sqlc.arg(scope_type)::text, sqlc.arg(custom_dept_ids)::bigint[])
ON CONFLICT (tenant_id, role_id, module)
DO UPDATE SET scope_type = excluded.scope_type, custom_dept_ids = excluded.custom_dept_ids;

-- name: ListRoleDataScopes :many
SELECT role_id, module, scope_type, custom_dept_ids
FROM role_data_scopes WHERE tenant_id = $1 ORDER BY role_id, module;

-- name: CreateTenant :one
INSERT INTO tenants (name) VALUES (sqlc.arg(name)::text)
RETURNING id, name, status;

-- name: AddTenantDomain :exec
-- Lower-cased on the way in so the login lookup, which lower-cases what it was
-- given, can be a plain primary-key hit.
INSERT INTO tenant_domains (domain, tenant_id)
VALUES (lower(sqlc.arg(domain)::text), sqlc.arg(tenant_id)::bigint)
ON CONFLICT (domain) DO NOTHING;

-- name: HasAnyTenant :one
SELECT EXISTS (SELECT 1 FROM tenants);

-- name: SetEmployeeEmailVerified :exec
UPDATE employees
SET email = sqlc.arg(email)::text, email_verified_at = now(), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: DeleteLiveInvitations :execrows
-- Kills whatever link this employee already holds, so re-inviting replaces
-- rather than accumulates. See the partial unique index for why.
DELETE FROM employee_invitations
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND employee_id = sqlc.arg(employee_id)::bigint
  AND used_at IS NULL;

-- name: CreateInvitation :one
INSERT INTO employee_invitations (tenant_id, employee_id, email, token_hash, expires_at, invited_by)
VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(employee_id)::bigint,
    lower(sqlc.arg(email)::text),
    sqlc.arg(token_hash),
    sqlc.arg(expires_at),
    sqlc.arg(invited_by)::bigint
)
RETURNING id, expires_at;

-- name: GetInvitationByToken :one
-- Everything activation needs in one round trip, including the employee's
-- current address so the service can refuse a link whose target was edited
-- after it was sent. Deliberately returns expired and used rows too: the page
-- has to tell somebody *why* their link does not work, and "expired" and
-- "already used" are different things to say.
SELECT i.id, i.tenant_id, i.employee_id, i.email, i.expires_at, i.used_at,
       e.name AS employee_name, e.status AS employee_status,
       e.email AS current_email, e.email_verified_at,
       t.status AS tenant_status
FROM employee_invitations i
JOIN employees e ON e.id = i.employee_id AND e.tenant_id = i.tenant_id
JOIN tenants t ON t.id = i.tenant_id
WHERE i.token_hash = sqlc.arg(token_hash);

-- name: ConsumeInvitation :execrows
-- The WHERE clause is the concurrency control: two requests arriving with the
-- same token race here, and exactly one updates a row. Checking "is it unused"
-- in Go and then updating would let both through.
UPDATE employee_invitations
SET used_at = now()
WHERE id = sqlc.arg(id)::bigint AND used_at IS NULL;

-- name: UpsertUserPassword :exec
-- Activation is the same operation whether or not an account already exists —
-- an employee imported with a login gets their password replaced, one imported
-- without gets a row. employee_id is UNIQUE, so the conflict target is exact.
INSERT INTO users (tenant_id, employee_id, username, password_hash)
VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(employee_id)::bigint,
    sqlc.arg(username)::text,
    sqlc.arg(password_hash)::text
)
ON CONFLICT (employee_id) DO UPDATE
SET password_hash = EXCLUDED.password_hash,
    status        = 'ACTIVE',
    failed_count  = 0,
    updated_at    = now();

-- name: IsTenantDomain :one
-- Whether an address belongs to this company. Invitations are refused for
-- anything else: an address on a domain the company does not own could not be
-- read by the company, so a link sent there proves nothing about employment.
SELECT EXISTS (
    SELECT 1 FROM tenant_domains
    WHERE domain = lower(sqlc.arg(domain)::text)
      AND tenant_id = sqlc.arg(tenant_id)::bigint
) AS owned;

-- name: ListLiveInvitations :many
-- Powers the employee list's status column: who is waiting on a link, and
-- until when. One query for the whole page rather than one per row, the same
-- shape as ListEmployeeAccounts beside it — the alternative is an N+1 on a
-- screen whose entire job during a migration is to be scanned.
SELECT employee_id, expires_at
FROM employee_invitations
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND used_at IS NULL;
