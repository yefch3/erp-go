-- name: GetUserByEmail :one
-- The whole of login's lookup, in one round trip and without asking the
-- domain anything.
--
-- The domain used to name the tenant, which only works while a domain belongs
-- to one company. gmail.com, qq.com and 163.com belong to everybody, and
-- tenant_domains.domain is a PRIMARY KEY — so the second company running on
-- QQ mail could not be onboarded at all. The address itself is now unique
-- system-wide (see 00023), so it identifies the account directly and the
-- account carries its own tenant_id.
--
-- tenants is joined rather than queried after, because a suspended company
-- has to be refused and a second round trip on every login attempt — including
-- every failed one — is a cost paid for nothing.
--
-- lower() on both sides: an address is case-insensitive in practice, and
-- "Alice@" must not be a second account from "alice@". email_verified_at rides
-- along because login has to refuse an account whose mailbox was never proved
-- to exist, and doing it here keeps that check free.
SELECT u.id, u.tenant_id, u.employee_id, u.username, u.password_hash, u.status, u.failed_count,
       u.locked_until, u.must_change_password,
       e.name AS employee_name, e.code AS employee_code, e.department_id,
       e.status AS employee_status, e.email_verified_at,
       t.status AS tenant_status
FROM users u
JOIN employees e ON e.id = u.employee_id
JOIN tenants t ON t.id = u.tenant_id
WHERE e.email <> ''
  AND lower(e.email) = lower(sqlc.arg(email)::text);

-- name: GetUserByUsername :one
SELECT u.id, u.tenant_id, u.employee_id, u.username, u.password_hash, u.status, u.failed_count,
       e.name AS employee_name, e.code AS employee_code, e.department_id, e.status AS employee_status
FROM users u
JOIN employees e ON e.id = u.employee_id
WHERE u.tenant_id = $1 AND u.username = $2;

-- name: RecordLoginSuccess :exec
-- Clears the deadline as well as the counter. Somebody who was locked at
-- 09:00 and signs in at 09:20 has demonstrated the thing the lock was waiting
-- to find out; leaving the timestamp behind would let a stale value refuse
-- their next attempt.
UPDATE users
SET failed_count = 0, locked_until = NULL, last_login_at = now(), updated_at = now()
WHERE id = $1;

-- name: RecordLoginFailure :one
-- Ten wrong passwords buy sixty seconds, and only ever pushed forward from
-- now — the eleventh does not extend it to two minutes. Without the
-- greatest(), somebody guessing keeps renewing the lock they put on another
-- person, which is the punishment landing on the wrong side.
--
-- The numbers are ERPNext's, and they are a pair rather than two choices. A
-- short window is what makes this survivable when it is aimed at somebody on
-- purpose: sixty seconds of being shut out, not a quarter of an hour. What
-- pays for it is that ten guesses a minute is only harmless against a
-- password worth having, which is why the strength policy landed in the same
-- change and why neither may be relaxed without the other.
UPDATE users
SET failed_count = failed_count + 1,
    locked_until = CASE
        WHEN failed_count + 1 >= 10 THEN greatest(locked_until, now() + interval '60 seconds')
        ELSE locked_until
    END,
    updated_at = now()
WHERE id = $1
RETURNING failed_count, locked_until;

-- name: CreateDepartment :one
INSERT INTO departments (tenant_id, code, name, parent_id, path, level)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetDepartment :one
SELECT * FROM departments WHERE tenant_id = $1 AND id = $2;

-- name: ListDepartments :many
SELECT * FROM departments WHERE tenant_id = $1 ORDER BY path, sort_order, id;

-- name: UpdateDepartmentDetails :one
UPDATE departments
SET code = sqlc.arg(code)::text,
    name = sqlc.arg(name)::text,
    parent_id = nullif(sqlc.arg(parent_id)::bigint, 0),
    sort_order = sqlc.arg(sort_order)::int,
    leader_employee_id = nullif(sqlc.arg(leader_employee_id)::bigint, 0),
    version = version + 1,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND version = sqlc.arg(expected_version)::int
RETURNING *;

-- name: UpdateDepartmentSubtree :exec
UPDATE departments
SET path = sqlc.arg(new_path)::text || substring(path FROM length(sqlc.arg(old_path)::text) + 1),
    level = level + sqlc.arg(level_delta)::int,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND path LIKE sqlc.arg(old_path)::text || '%';

-- name: SetDepartmentStatus :one
UPDATE departments
SET status = sqlc.arg(status)::text,
    version = version + 1,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND version = sqlc.arg(expected_version)::int
RETURNING *;

-- name: CountActiveEmployeesInDepartment :one
SELECT count(*) FROM employees
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND department_id = sqlc.arg(department_id)::bigint
  AND status = 'ACTIVE';

-- name: CountActiveChildDepartments :one
SELECT count(*) FROM departments
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND parent_id = sqlc.arg(parent_id)::bigint
  AND status = 'ACTIVE';

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

-- name: ListEmployeesFiltered :many
SELECT e.*, d.name AS department_name, coalesce(m.name, '')::text AS manager_name,
       count(*) OVER () AS total
FROM employees e
JOIN departments d ON d.id = e.department_id AND d.tenant_id = e.tenant_id
LEFT JOIN employees m ON m.id = e.manager_id AND m.tenant_id = e.tenant_id
LEFT JOIN users u ON u.employee_id = e.id AND u.tenant_id = e.tenant_id
WHERE e.tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(department_id)::bigint = 0 OR e.department_id = sqlc.arg(department_id)::bigint)
  AND (sqlc.arg(manager_id)::bigint = 0 OR e.manager_id = sqlc.arg(manager_id)::bigint)
  AND (sqlc.arg(role_id)::bigint = 0 OR EXISTS (
    SELECT 1 FROM employee_roles er
    WHERE er.tenant_id = e.tenant_id AND er.employee_id = e.id
      AND er.role_id = sqlc.arg(role_id)::bigint
  ))
  AND (sqlc.arg(employment_status)::text = '' OR e.status = sqlc.arg(employment_status)::text)
  AND (
    sqlc.arg(account_status)::text = ''
    OR (sqlc.arg(account_status)::text = 'NONE' AND u.id IS NULL AND NOT EXISTS (
      SELECT 1 FROM employee_invitations i
      WHERE i.tenant_id = e.tenant_id AND i.employee_id = e.id AND i.used_at IS NULL
    ))
    OR (sqlc.arg(account_status)::text = 'PENDING' AND e.email_verified_at IS NULL AND EXISTS (
      SELECT 1 FROM employee_invitations i
      WHERE i.tenant_id = e.tenant_id AND i.employee_id = e.id AND i.used_at IS NULL
    ))
    OR (sqlc.arg(account_status)::text = 'ACTIVE' AND u.status = 'ACTIVE' AND e.email_verified_at IS NOT NULL)
  )
  AND (
    sqlc.arg(keyword)::text = ''
    OR e.name ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR e.english_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR e.code ILIKE '%' || sqlc.arg(keyword)::text || '%'
    OR e.email ILIKE '%' || sqlc.arg(keyword)::text || '%'
  )
ORDER BY e.id DESC
LIMIT sqlc.arg(page_size)::int OFFSET sqlc.arg(page_offset)::int;

-- name: UpdateEmployeeDetails :one
UPDATE employees
SET code = sqlc.arg(code)::text,
    name = sqlc.arg(name)::text,
    english_name = sqlc.arg(english_name)::text,
    department_id = sqlc.arg(department_id)::bigint,
    position = sqlc.arg(position)::text,
    email = sqlc.arg(email)::text,
    phone = sqlc.arg(phone)::text,
    manager_id = nullif(sqlc.arg(manager_id)::bigint, 0),
    hire_date = sqlc.narg(hire_date)::date,
    leave_date = sqlc.narg(leave_date)::date,
    remark = sqlc.arg(remark)::text,
    version = version + 1,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND version = sqlc.arg(expected_version)::int
RETURNING *;

-- name: UpdateOwnProfile :one
-- 员工改自己的资料。字段白名单写死在 SQL 里，不是在 Go 里过滤——
-- 一条只能改这两列的语句，比一个「记得别把别的字段传进来」的约定可靠。
--
-- 带版本号，和 UpdateEmployeeDetails 用同一把锁：管理员正在改这个人的资料时
-- 员工按了保存，应该是「有人改过了，请刷新」，而不是谁后写谁赢。
UPDATE employees
SET english_name = sqlc.arg(english_name)::text,
    phone = sqlc.arg(phone)::text,
    version = version + 1,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND version = sqlc.arg(expected_version)::int
RETURNING *;

-- name: SetEmployeeAvatar :one
-- 头像单独一条，不并进 UpdateEmployeeDetails，也不并进 UpdateOwnProfile。
--
-- 并进去的话，任何一个忘了回传 avatar_key 的表单提交都会把头像清空——
-- 而「少传一个字段就悄悄删数据」这种形状，这个代码库里已经栽过。传图片本来
-- 也不是填表：点头像、选文件、传完就生效，和按「保存」是两个动作。
--
-- 不动 version：头像不属于那张表单的乐观锁范围，管理员开着表单时员工换了张
-- 照片，不该让管理员的保存失败。
UPDATE employees
SET avatar_key = sqlc.arg(avatar_key)::text, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
RETURNING *;

-- name: ListOrgChartMembers :many
-- 画组织架构图用的全量员工。
--
-- 不分页：三百人的图就是要一次画完，分页的树是画不出来的（父节点在第 1 页、
-- 子节点在第 3 页，那还叫什么树）。真到了分页才画得动的规模，要换的是
-- 「按部门懒加载」这种别的做法，不是给这条加 LIMIT。
--
-- 字段比 ListEmployees 少一大截：不联账号、不查邀请状态——图上不显示那些，
-- 而每多一个联表就是三百行乘一次。
--
-- 只要在职的。离职的人留在图上会让「这个部门有几个人」永远算不对，
-- 而想看历史的人要的是变更记录，不是一张混着离职者的架构图。
SELECT e.id, e.code, e.name, e.english_name, e.position, e.status,
       e.manager_id, e.department_id, d.name AS department_name,
       e.avatar_key, e.leave_date
FROM employees e
JOIN departments d ON d.id = e.department_id AND d.tenant_id = e.tenant_id
WHERE e.tenant_id = sqlc.arg(tenant_id)::bigint
  AND e.status = 'ACTIVE'
ORDER BY d.path, e.id;

-- name: ListEmployeeAvatars :many
-- 一批人的头像 key。列表页和组织架构图一次要显示几十上百张，
-- 逐个去查就是逐个往返。没有头像的人也返回（key 是空串），由调用方跳过——
-- 在 SQL 里过滤会让「这个人查到了但没头像」和「这个人根本不在」变成同一件事。
SELECT id, avatar_key FROM employees
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = ANY(sqlc.arg(ids)::bigint[]);

-- name: ManagerCycleExists :one
WITH RECURSIVE chain AS (
  SELECT id, manager_id, ARRAY[id]::bigint[] AS visited
  FROM employees
  WHERE tenant_id = sqlc.arg(tenant_id)::bigint
    AND id = sqlc.arg(manager_id)::bigint
  UNION ALL
  SELECT e.id, e.manager_id, c.visited || e.id
  FROM employees e
  JOIN chain c ON e.id = c.manager_id
  WHERE e.tenant_id = sqlc.arg(tenant_id)::bigint
    AND NOT e.id = ANY(c.visited)
)
SELECT EXISTS (
  SELECT 1 FROM chain WHERE id = sqlc.arg(employee_id)::bigint
);

-- name: CountActiveDirectReports :one
SELECT count(*) FROM employees
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND manager_id = sqlc.arg(manager_id)::bigint
  AND status = 'ACTIVE';

-- name: CountDepartmentsLedByEmployee :one
SELECT count(*) FROM departments
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND leader_employee_id = sqlc.arg(employee_id)::bigint
  AND status = 'ACTIVE';

-- name: InsertDirectoryChange :exec
INSERT INTO directory_change_logs (
  tenant_id, entity_type, entity_id, action, before_data, after_data, operator_id
) VALUES (
  sqlc.arg(tenant_id)::bigint,
  sqlc.arg(entity_type)::text,
  sqlc.arg(entity_id)::bigint,
  sqlc.arg(action)::text,
  sqlc.arg(before_data)::jsonb,
  sqlc.arg(after_data)::jsonb,
  sqlc.arg(operator_id)::bigint
);

-- name: ListDirectoryChanges :many
SELECT * FROM directory_change_logs
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND entity_type = sqlc.arg(entity_type)::text
  AND entity_id = sqlc.arg(entity_id)::bigint
ORDER BY id DESC
LIMIT 100;

-- name: DeactivateEmployee :execrows
UPDATE employees SET status = 'INACTIVE', leave_date = current_date, version = version + 1, updated_at = now()
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
-- 只列启用的：这是别处（审批按编码找角色、员工分配角色）依赖的语义。
SELECT * FROM roles WHERE tenant_id = $1 AND status = 'ACTIVE' ORDER BY id;

-- name: ListRolesIncludingInactive :many
-- 角色管理页专用：停用的也要看得见，否则停掉之后没有任何入口能把它启用回来。
SELECT * FROM roles WHERE tenant_id = $1 ORDER BY status DESC, id;

-- name: GetRole :one
SELECT * FROM roles WHERE tenant_id = $1 AND id = $2;

-- name: CountRoleHolders :one
-- 还有几个在职员工持有这个角色。停用会当场收走他们的权限，所以这个数字要在
-- 停用之前摆到人眼前，而不是之后由他们来报「我打不开页面了」。
SELECT count(*)::bigint FROM employee_roles er
JOIN employees e ON e.id = er.employee_id AND e.tenant_id = er.tenant_id
WHERE er.tenant_id = $1 AND er.role_id = $2 AND e.status = 'ACTIVE';

-- name: SetRoleStatus :execrows
UPDATE roles SET status = sqlc.arg(status)::text
WHERE tenant_id = $1 AND id = sqlc.arg(id)::bigint;

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

-- name: ListPermissionCodesOfRoles :many
-- 一次问完一批角色的权限码。
--
-- 角色列表原来对每个角色单独查一次（十来个角色就是十来次往返）。角色数量
-- 不大，所以这不是性能事故，但同一个形状在别处会是——一次问完是这一类查询
-- 该有的样子。
SELECT rp.role_id, p.code
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
WHERE rp.tenant_id = sqlc.arg(tenant_id)::bigint
  AND rp.role_id = ANY(sqlc.arg(role_ids)::bigint[])
ORDER BY rp.role_id, p.code;

-- name: ReplaceEmployeeRoles :exec
DELETE FROM employee_roles WHERE tenant_id = $1 AND employee_id = $2;

-- name: AddEmployeeRole :exec
INSERT INTO employee_roles (tenant_id, employee_id, role_id)
VALUES ($1, $2, $3) ON CONFLICT DO NOTHING;

-- name: ListEmployeeRoleIDs :many
SELECT role_id FROM employee_roles WHERE tenant_id = $1 AND employee_id = $2 ORDER BY role_id;

-- name: ListEmployeePermissionCodes :many
-- 同 EmployeeHasPermission：停用的角色不再给人任何权限。这条是登录时算
-- 菜单用的，两处必须同口径——否则菜单亮着、点进去 403。
SELECT DISTINCT p.code
FROM employee_roles er
JOIN employees e ON e.id = er.employee_id AND e.tenant_id = er.tenant_id
JOIN roles r ON r.id = er.role_id AND r.tenant_id = er.tenant_id
JOIN role_permissions rp ON rp.tenant_id = er.tenant_id AND rp.role_id = er.role_id
JOIN permissions p ON p.id = rp.permission_id
WHERE er.tenant_id = $1 AND er.employee_id = $2
  AND e.status = 'ACTIVE' AND r.status = 'ACTIVE'
ORDER BY p.code;

-- name: EmployeeHasPermission :one
-- The employee join is not decoration: without it a token issued before
-- someone left keeps working until it expires. Every guarded request runs
-- through here, so this is where "left the company" takes effect.
--
-- The roles join is the same idea one level up, and it was missing: a role
-- flipped to INACTIVE vanished from the roles page while everybody holding
-- it kept every permission it granted. A button that says "停用" and takes
-- nothing away is worse than no button — the administrator believes access
-- was revoked and stops looking.
SELECT EXISTS (
    SELECT 1
    FROM employee_roles er
    JOIN employees e ON e.id = er.employee_id AND e.tenant_id = er.tenant_id
    JOIN roles r ON r.id = er.role_id AND r.tenant_id = er.tenant_id
    JOIN role_permissions rp ON rp.tenant_id = er.tenant_id AND rp.role_id = er.role_id
    JOIN permissions p ON p.id = rp.permission_id
    WHERE er.tenant_id = $1 AND er.employee_id = $2 AND p.code = $3
      AND e.status = 'ACTIVE' AND r.status = 'ACTIVE'
) AS allowed;

-- name: HasAnyUser :one
-- EXISTS stops at the first row: O(1) regardless of table size, unlike count(*).
SELECT EXISTS (SELECT 1 FROM users WHERE tenant_id = $1) AS has_users;

-- name: SetDepartmentPath :exec
UPDATE departments SET path = $3, level = $4 WHERE tenant_id = $1 AND id = $2;

-- name: ListRoleMembers :many
-- 停用的角色不再供出成员：审批流指着它时，拿到空名单会明确报「审批节点没有
-- 可用审批人」，而不是把任务派给一个公司已经废弃的角色。
SELECT e.id AS employee_id, e.name
FROM employee_roles er
JOIN employees e ON e.id = er.employee_id AND e.tenant_id = er.tenant_id
JOIN roles r ON r.id = er.role_id AND r.tenant_id = er.tenant_id
WHERE er.tenant_id = $1 AND er.role_id = $2
  AND e.status = 'ACTIVE' AND r.status = 'ACTIVE'
ORDER BY e.id;

-- name: GetUserByEmployee :one
SELECT id, username, password_hash, status FROM users
WHERE tenant_id = $1 AND employee_id = $2;

-- name: UpdatePassword :execrows
-- Unlocks, which it did not before. It reset failed_count and left the lock
-- untouched, so an administrator clicking 重置密码 on a locked account got a
-- success message and changed nothing that person could feel. A new password
-- is a stronger statement than a fifteen-minute wait; it has to clear it.
UPDATE users
SET password_hash = $3, failed_count = 0, locked_until = NULL, updated_at = now()
WHERE tenant_id = $1 AND employee_id = $2;

-- name: ListEmployeeAccounts :many
-- Which employees can log in, for the employee list; a company usually has
-- more employees than accounts.
SELECT employee_id, username FROM users WHERE tenant_id = $1;

-- name: ActivateEmployee :execrows
UPDATE employees SET status = 'ACTIVE', leave_date = NULL, version = version + 1, updated_at = now()
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
-- 停用的角色不再放宽任何人的可见范围：权限收回了、范围还留着，等于人看得见
-- 一堆自己再也打不开的单据。
SELECT s.scope_type, s.custom_dept_ids
FROM role_data_scopes s
JOIN employee_roles er ON er.role_id = s.role_id AND er.tenant_id = s.tenant_id
JOIN roles r ON r.id = s.role_id AND r.tenant_id = s.tenant_id
WHERE s.tenant_id = $1 AND er.employee_id = $2 AND s.module = $3
  AND r.status = 'ACTIVE'
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
    locked_until  = NULL,
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

-- name: ListEmployeeIdentity :many
-- Every code and address already taken in this company, for the import to
-- check a whole pasted block against in one round trip rather than a query
-- per row. A company has hundreds of employees, not millions; the cost of
-- reading them all is far below the cost of an N+1 on a screen somebody is
-- watching while their 200-row paste validates.
SELECT id, code, lower(email)::text AS email
FROM employees
WHERE tenant_id = sqlc.arg(tenant_id)::bigint;

-- name: ListTenantDomains :many
-- The mail domains this company owns. The import refuses an address outside
-- them, because one can never be activated — the link would go to a mailbox
-- the company cannot read — and importing it only defers that discovery to
-- the day somebody wonders why forty people never got invited.
SELECT domain FROM tenant_domains
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
ORDER BY domain;

-- name: GetEmployeeByCode :one
SELECT id, name FROM employees
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND code = sqlc.arg(code)::text;

-- name: DomainClaimed :one
-- 开第二家公司时的幂等键：域名已归属任何一家就不再开。
SELECT EXISTS (SELECT 1 FROM tenant_domains WHERE domain = $1::text) AS claimed;

-- name: IsPlatformOperator :one
-- 平台身份住在权限系统之外，理由见 00044 的表注释。
SELECT EXISTS (SELECT 1 FROM platform_operators WHERE employee_id = $1) AS ok;

-- name: ListTenantsForPlatform :many
-- 开户页的清单：每家公司一行，带管理员地址和「激活了没有」。
-- 管理员按 code='ADMIN' 找——两条开户路径（引导种子与平台开户）写的都是它。
SELECT t.id, t.name, t.status, t.created_at,
       coalesce(a.email, '')::text AS admin_email,
       coalesce(a.email_verified_at IS NOT NULL, false)::boolean AS admin_activated
FROM tenants t
LEFT JOIN employees a ON a.tenant_id = t.id AND a.code = 'ADMIN'
ORDER BY t.id;

-- name: FindTenantAdmin :one
-- 重发邀请要找的人。
SELECT id, email FROM employees
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND code = 'ADMIN';

-- name: SetTenantStatus :execrows
UPDATE tenants SET status = sqlc.arg(status)::text, updated_at = now()
WHERE id = sqlc.arg(id)::bigint;

-- name: RoleExistsByCode :one
-- 任何状态都算：角色只能停用不能删除，「有但停用了」是有人做过的决定，
-- 补种不该把它当成「没有」。
SELECT EXISTS (
    SELECT 1 FROM roles WHERE tenant_id = $1 AND code = $2
);

-- name: ListTenantIDs :many
SELECT id FROM tenants ORDER BY id;
