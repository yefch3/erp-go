-- +goose Up
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('procurement:sourcing:approve', '确认成本方案并生成客户报价', 'procurement', '')
ON CONFLICT (code) DO NOTHING;
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code='SUPER_ADMIN' AND p.code='procurement:sourcing:approve'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id=(SELECT id FROM permissions WHERE code='procurement:sourcing:approve');
DELETE FROM permissions WHERE code='procurement:sourcing:approve';
