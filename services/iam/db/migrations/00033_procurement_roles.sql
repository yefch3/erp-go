-- +goose Up

INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('procurement:requirement:exception', '建立例外采购需求', 'procurement', ''),
  ('procurement:order:submit',          '提交采购单审批',   'procurement', ''),
  ('procurement:order:cancel',          '取消采购单',       'procurement', ''),
  ('procurement:receipt:write',         '登记采购到货',     'procurement', ''),
  ('procurement:payment:read',          '查看采购付款',     'procurement', ''),
  ('procurement:payment:write',         '维护采购付款',     'procurement', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO roles (tenant_id, code, name, description) VALUES
  (1, 'BUYER', '采购专员', '询价、匹配采购需求、维护采购单草稿并提交审批'),
  (1, 'PROCUREMENT_MANAGER', '采购经理', '管理采购询价、例外需求、采购单审批与取消')
ON CONFLICT (tenant_id, code) DO NOTHING;

-- Administrators remain the break-glass role and receive every new action.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN'
  AND p.code IN (
    'procurement:requirement:exception', 'procurement:order:submit',
    'procurement:order:cancel', 'procurement:receipt:write',
    'procurement:payment:read', 'procurement:payment:write'
  )
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'BUYER'
  AND p.code IN (
    'procurement:sourcing:read', 'procurement:sourcing:write',
    'procurement:sourcing:send', 'procurement:sourcing:price',
    'procurement:requirement:read',
    'procurement:order:read', 'procurement:order:write', 'procurement:order:submit',
    'masterdata:supplier:read', 'product:product:read'
  )
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'PROCUREMENT_MANAGER'
  AND p.code IN (
    'procurement:sourcing:read', 'procurement:sourcing:write',
    'procurement:sourcing:send', 'procurement:sourcing:price', 'procurement:sourcing:approve',
    'procurement:requirement:read', 'procurement:requirement:write', 'procurement:requirement:exception',
    'procurement:order:read', 'procurement:order:write', 'procurement:order:submit', 'procurement:order:cancel',
    'procurement:payment:read', 'masterdata:supplier:read', 'product:product:read',
    'approval:task:act'
  )
ON CONFLICT DO NOTHING;

-- Warehouse staff can record arrival without gaining the power to edit the
-- supplier, quantity or price on an order. Finance owns payment records but
-- cannot change purchasing terms.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE (r.code = 'LOGISTICS' AND p.code = 'procurement:receipt:write')
   OR (r.code = 'FINANCE' AND p.code IN ('procurement:payment:read', 'procurement:payment:write'))
ON CONFLICT DO NOTHING;

INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT tenant_id, id, 'procurement_sourcing', 'ALL'
FROM roles WHERE code IN ('BUYER', 'PROCUREMENT_MANAGER')
ON CONFLICT (tenant_id, role_id, module)
DO UPDATE SET scope_type = 'ALL', custom_dept_ids = '{}';

-- +goose Down
DELETE FROM role_data_scopes
WHERE role_id IN (SELECT id FROM roles WHERE code IN ('BUYER', 'PROCUREMENT_MANAGER'))
  AND module = 'procurement_sourcing';
DELETE FROM role_permissions
WHERE role_id IN (SELECT id FROM roles WHERE code IN ('BUYER', 'PROCUREMENT_MANAGER'))
   OR permission_id IN (SELECT id FROM permissions WHERE code IN (
      'procurement:requirement:exception', 'procurement:order:submit',
      'procurement:order:cancel', 'procurement:receipt:write',
      'procurement:payment:read', 'procurement:payment:write'
   ));
DELETE FROM roles WHERE code IN ('BUYER', 'PROCUREMENT_MANAGER');
DELETE FROM permissions WHERE code IN (
  'procurement:requirement:exception', 'procurement:order:submit',
  'procurement:order:cancel', 'procurement:receipt:write',
  'procurement:payment:read', 'procurement:payment:write'
);
