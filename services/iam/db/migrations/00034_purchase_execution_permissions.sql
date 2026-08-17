-- +goose Up

INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('procurement:order:send', '正式发送采购单', 'procurement', ''),
  ('procurement:production:write', '维护采购生产进度', 'procurement', ''),
  ('procurement:exception:write', '维护采购到货异常', 'procurement', '')
ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, module = EXCLUDED.module;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('SUPER_ADMIN', 'ADMIN')
  AND p.code IN ('procurement:order:send', 'procurement:production:write', 'procurement:exception:write')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE (r.code IN ('BUYER', 'PROCUREMENT_MANAGER') AND p.code IN ('procurement:order:send', 'procurement:production:write', 'procurement:exception:write'))
   OR (r.code = 'LOGISTICS' AND p.code = 'procurement:exception:write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (
  SELECT id FROM permissions WHERE code IN ('procurement:order:send', 'procurement:production:write', 'procurement:exception:write')
);
DELETE FROM permissions WHERE code IN ('procurement:order:send', 'procurement:production:write', 'procurement:exception:write');
