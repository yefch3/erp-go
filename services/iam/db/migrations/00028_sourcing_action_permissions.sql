-- +goose Up
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('procurement:sourcing:send',  '发送工厂询价', 'procurement', ''),
  ('procurement:sourcing:price', '维护供应商报价', 'procurement', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN'
  AND p.code IN ('procurement:sourcing:send', 'procurement:sourcing:price')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (
  SELECT id FROM permissions WHERE code IN ('procurement:sourcing:send', 'procurement:sourcing:price')
);
DELETE FROM permissions WHERE code IN ('procurement:sourcing:send', 'procurement:sourcing:price');
