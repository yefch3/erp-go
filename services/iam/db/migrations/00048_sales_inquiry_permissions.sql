-- +goose Up

-- tenant-seed: 第一家公司补齐标准销售角色；其他公司由 IAM 预置角色逻辑补齐。

-- SP2：客户询盘属于销售，采购只接收已经复核的需求快照。
-- 独立权限避免为了看采购进度而把供应商底价一并开放给销售。
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('sales:inquiry:read',              '查看客户询盘',       'sales', '/sales/inquiries'),
  ('sales:inquiry:write',             '维护客户询盘',       'sales', ''),
  ('sales:inquiry:submit',            '提交客户询盘给采购', 'sales', ''),
  ('sales:procurement-progress:read', '查看采购进度摘要',   'sales', '')
ON CONFLICT (code) DO NOTHING;

-- 与既有预置角色迁移一致：第一家公司也拥有标准销售专员。
-- 只有本迁移新建角色时才附加旧权限；客户已经创建并调整过的同编码角色只会在
-- 下方获得本次新增的 sales:* 权限，不会被重置成预置权限。
WITH inserted_sales AS (
  INSERT INTO roles (tenant_id, code, name, description) VALUES
    (1, 'SALES', '销售专员', '维护本人客户询盘、客户报价和出口合同')
  ON CONFLICT (tenant_id, code) DO NOTHING
  RETURNING tenant_id, id
)
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM inserted_sales r CROSS JOIN permissions p
WHERE p.code IN (
  'export:quotation:read', 'export:quotation:write',
  'export:contract:read', 'export:contract:write',
  'masterdata:customer:read', 'product:product:read'
)
ON CONFLICT DO NOTHING;

-- 超管与销售经理获得新的销售权限；旧只读销售保持只读。
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE (r.code IN ('SUPER_ADMIN', 'SALES_MANAGER') AND p.code LIKE 'sales:%')
   OR (r.code = 'SALES_RO' AND p.code IN (
      'sales:inquiry:read', 'sales:procurement-progress:read'
   ))
ON CONFLICT DO NOTHING;

-- 现有销售专员只追加本次新建的销售权限，不修改既有定制授权。
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SALES'
  AND p.code IN (
    'sales:inquiry:read', 'sales:inquiry:write', 'sales:inquiry:submit',
    'sales:procurement-progress:read'
  )
ON CONFLICT DO NOTHING;

-- 询盘底层暂时复用采购寻源的属主字段，因此复用这一数据范围名称；
-- 这不会给销售授予任何 procurement:sourcing:* 功能权限。
INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT r.tenant_id, r.id, 'procurement_sourcing',
       CASE WHEN r.code = 'SALES_MANAGER' THEN 'DEPT_AND_SUB' ELSE 'SELF' END
FROM roles r
WHERE r.code IN ('SALES', 'SALES_MANAGER', 'SALES_RO')
ON CONFLICT (tenant_id, role_id, module) DO UPDATE
SET scope_type = EXCLUDED.scope_type;

-- 客户报价与出口合同同样按销售组织范围隔离：专员看本人，经理看本部门及下级。
INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT r.tenant_id, r.id, 'export',
       CASE WHEN r.code = 'SALES_MANAGER' THEN 'DEPT_AND_SUB' ELSE 'SELF' END
FROM roles r
WHERE r.code IN ('SALES', 'SALES_MANAGER')
ON CONFLICT (tenant_id, role_id, module) DO UPDATE
SET scope_type = EXCLUDED.scope_type;

-- +goose Down
DELETE FROM role_data_scopes
WHERE module IN ('procurement_sourcing', 'export')
  AND role_id IN (SELECT id FROM roles WHERE code = 'SALES');
UPDATE role_data_scopes
SET scope_type = 'SELF'
WHERE module = 'procurement_sourcing'
  AND role_id IN (SELECT id FROM roles WHERE code = 'SALES_MANAGER');
UPDATE role_data_scopes
SET scope_type = 'ALL'
WHERE module = 'export'
  AND role_id IN (SELECT id FROM roles WHERE code = 'SALES_MANAGER');
DELETE FROM role_permissions
WHERE role_id IN (SELECT id FROM roles WHERE code = 'SALES')
   OR permission_id IN (SELECT id FROM permissions WHERE code LIKE 'sales:%');
DELETE FROM roles WHERE code = 'SALES';
DELETE FROM permissions WHERE code LIKE 'sales:%';
