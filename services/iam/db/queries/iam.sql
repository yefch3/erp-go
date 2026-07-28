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
INSERT INTO employees (tenant_id, code, name, department_id, position, email, phone)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetEmployee :one
SELECT e.*, d.name AS department_name
FROM employees e
JOIN departments d ON d.id = e.department_id
WHERE e.tenant_id = $1 AND e.id = $2;

-- name: ListEmployees :many
SELECT e.*, d.name AS department_name, count(*) OVER () AS total
FROM employees e
JOIN departments d ON d.id = e.department_id
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
JOIN role_permissions rp ON rp.tenant_id = er.tenant_id AND rp.role_id = er.role_id
JOIN permissions p ON p.id = rp.permission_id
WHERE er.tenant_id = $1 AND er.employee_id = $2
ORDER BY p.code;

-- name: EmployeeHasPermission :one
SELECT EXISTS (
    SELECT 1
    FROM employee_roles er
    JOIN role_permissions rp ON rp.tenant_id = er.tenant_id AND rp.role_id = er.role_id
    JOIN permissions p ON p.id = rp.permission_id
    WHERE er.tenant_id = $1 AND er.employee_id = $2 AND p.code = $3
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
