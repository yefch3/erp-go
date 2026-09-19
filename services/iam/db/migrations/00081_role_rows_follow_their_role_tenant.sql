-- +goose Up

-- 角色的权限行、范围行、员工挂角色的行，公司号必须和角色本身的公司号一致。
--
-- 2026-09-19 查出来的：迁移 00055 给「销售专员」「销售经理」补两条权限
-- （iam:employee:read、masterdata:supplier:read）时列清单里没写 tenant_id，
-- 数据库按默认值填成了 1。公司 1 恰好对；公司 2、3、4 的那几行都记在了
-- 公司 1 名下——而算"这个人有什么权限"的查询按本公司找，找不到，等于没给。
-- 公司 4 的销售于是进得了页面、看得到单子，但选负责人、选供应商那些
-- 动作出不来。生产库里一共 12 行（三家公司 × 两个角色 × 两条权限）。
--
-- 三步：先把记错的行改回各自角色的公司；再给这几张表加复合外键，让
-- "公司号和角色对不上"的行**写不进去**——scripts/check-iam-seeds.sh 那道
-- 检查只拦字面量 `SELECT 1`，拦不住"列清单里干脆没写 tenant_id"，而这一次
-- 就是后者。约束在库里，谁来写都一样。

-- ---- 1. role_permissions：改回角色的公司。同公司已有一模一样的行就删掉
--         错的那行（改过去会撞主键）。生产上没有这种情况，防御性写法。
DELETE FROM role_permissions rp
USING roles r
WHERE r.id = rp.role_id AND rp.tenant_id <> r.tenant_id
  AND EXISTS (SELECT 1 FROM role_permissions x
              WHERE x.role_id = rp.role_id AND x.permission_id = rp.permission_id
                AND x.tenant_id = r.tenant_id);
UPDATE role_permissions rp
SET tenant_id = r.tenant_id
FROM roles r
WHERE r.id = rp.role_id AND rp.tenant_id <> r.tenant_id;

-- ---- 2. role_data_scopes：同样处理。
DELETE FROM role_data_scopes s
USING roles r
WHERE r.id = s.role_id AND s.tenant_id <> r.tenant_id
  AND EXISTS (SELECT 1 FROM role_data_scopes x
              WHERE x.role_id = s.role_id AND x.module = s.module
                AND x.tenant_id = r.tenant_id);
UPDATE role_data_scopes s
SET tenant_id = r.tenant_id
FROM roles r
WHERE r.id = s.role_id AND s.tenant_id <> r.tenant_id;

-- ---- 3. employee_roles：人和角色不在同一家公司的，不是记错，是跨公司
--         挂角色，直接删；只是公司号记错的，改回人的公司。
DELETE FROM employee_roles er
USING roles r, employees e
WHERE r.id = er.role_id AND e.id = er.employee_id AND r.tenant_id <> e.tenant_id;
DELETE FROM employee_roles er
USING employees e
WHERE e.id = er.employee_id AND er.tenant_id <> e.tenant_id
  AND EXISTS (SELECT 1 FROM employee_roles x
              WHERE x.employee_id = er.employee_id AND x.role_id = er.role_id
                AND x.tenant_id = e.tenant_id);
UPDATE employee_roles er
SET tenant_id = e.tenant_id
FROM employees e
WHERE e.id = er.employee_id AND er.tenant_id <> e.tenant_id;

-- ---- 4. 约束。复合外键要求被引用的一侧有 (tenant_id, id) 的唯一键。
ALTER TABLE roles     ADD CONSTRAINT roles_tenant_id_id_key     UNIQUE (tenant_id, id);
ALTER TABLE employees ADD CONSTRAINT employees_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE role_permissions
  ADD CONSTRAINT role_permissions_role_tenant_fk
  FOREIGN KEY (tenant_id, role_id) REFERENCES roles (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE role_data_scopes
  ADD CONSTRAINT role_data_scopes_role_tenant_fk
  FOREIGN KEY (tenant_id, role_id) REFERENCES roles (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE employee_roles
  ADD CONSTRAINT employee_roles_role_tenant_fk
  FOREIGN KEY (tenant_id, role_id) REFERENCES roles (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE employee_roles
  ADD CONSTRAINT employee_roles_employee_tenant_fk
  FOREIGN KEY (tenant_id, employee_id) REFERENCES employees (tenant_id, id) ON DELETE CASCADE;

-- +goose Down

-- 改回去的行不再改回错的；只撤约束。
ALTER TABLE employee_roles   DROP CONSTRAINT IF EXISTS employee_roles_employee_tenant_fk;
ALTER TABLE employee_roles   DROP CONSTRAINT IF EXISTS employee_roles_role_tenant_fk;
ALTER TABLE role_data_scopes DROP CONSTRAINT IF EXISTS role_data_scopes_role_tenant_fk;
ALTER TABLE role_permissions DROP CONSTRAINT IF EXISTS role_permissions_role_tenant_fk;
ALTER TABLE employees        DROP CONSTRAINT IF EXISTS employees_tenant_id_id_key;
ALTER TABLE roles            DROP CONSTRAINT IF EXISTS roles_tenant_id_id_key;
