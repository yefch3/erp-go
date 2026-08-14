-- +goose Up
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('procurement:sourcing:read',  '查看采购询价', 'procurement', '/sourcing-cases'),
  ('procurement:sourcing:write', '维护采购询价', 'procurement', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN'
  AND p.code IN ('procurement:sourcing:read', 'procurement:sourcing:write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE code LIKE 'procurement:sourcing:%');
DELETE FROM permissions WHERE code LIKE 'procurement:sourcing:%';
