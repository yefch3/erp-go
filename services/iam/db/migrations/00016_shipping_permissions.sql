-- +goose Up

INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('shipping:schedule:read',  '查看船期', 'shipping', '/shipping'),
  ('shipping:schedule:write', '维护船期', 'shipping', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('SUPER_ADMIN', 'LOGISTICS')
  AND p.code IN ('shipping:schedule:read', 'shipping:schedule:write')
ON CONFLICT DO NOTHING;

INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT 1, r.id, 'shipping', 'ALL'
FROM roles r
WHERE r.code IN ('SUPER_ADMIN', 'LOGISTICS')
ON CONFLICT (tenant_id, role_id, module) DO NOTHING;

-- +goose Down
DELETE FROM role_data_scopes
WHERE module = 'shipping'
  AND role_id IN (SELECT id FROM roles WHERE code IN ('SUPER_ADMIN', 'LOGISTICS'));
DELETE FROM role_permissions WHERE permission_id IN (
  SELECT id FROM permissions
  WHERE code IN ('shipping:schedule:read', 'shipping:schedule:write'));
DELETE FROM permissions
WHERE code IN ('shipping:schedule:read', 'shipping:schedule:write');
