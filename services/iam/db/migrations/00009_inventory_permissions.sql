-- +goose Up
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('inventory:stock:read',  '查看库存', 'inventory', '/stocks'),
  ('inventory:stock:write', '出入库操作', 'inventory', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN'
  AND p.code IN ('inventory:stock:read', 'inventory:stock:write')
ON CONFLICT DO NOTHING;

-- Sales need to see stock to answer "can we ship by then"; they must not move it.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('SALES_MANAGER', 'SALES_RO') AND p.code = 'inventory:stock:read'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE code LIKE 'inventory:stock:%');
DELETE FROM permissions WHERE code LIKE 'inventory:stock:%';
