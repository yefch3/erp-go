-- +goose Up

INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('shipping:route:write',    '维护船期港口路线', 'shipping', ''),
  ('shipping:progress:write', '更新船期进度和延误', 'shipping', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('SUPER_ADMIN', 'LOGISTICS')
  AND p.code IN ('shipping:route:write', 'shipping:progress:write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (
  SELECT id FROM permissions WHERE code IN ('shipping:route:write', 'shipping:progress:write'));
DELETE FROM permissions WHERE code IN ('shipping:route:write', 'shipping:progress:write');
