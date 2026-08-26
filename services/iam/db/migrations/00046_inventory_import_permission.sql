-- +goose Up

INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('inventory:stock:import', '导入期初库存', 'inventory', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN' AND p.code = 'inventory:stock:import'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions rp USING permissions p
WHERE rp.permission_id = p.id AND p.code = 'inventory:stock:import';
DELETE FROM permissions WHERE code = 'inventory:stock:import';
