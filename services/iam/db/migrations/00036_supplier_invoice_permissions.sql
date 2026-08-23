-- +goose Up

-- Supplier invoices get their own permission rather than riding on the order
-- one, for the same reason orders did not ride on requirements: recording
-- what a factory claims we owe is the doorway to paying it, and in most
-- companies the person who places orders and the person who registers
-- invoices for payment are deliberately not the same person.
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('procurement:invoice:read',  '查看供应商发票', 'procurement', '/supplier-invoices'),
  ('procurement:invoice:write', '维护供应商发票', 'procurement', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN'
  AND p.code IN ('procurement:invoice:read', 'procurement:invoice:write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE code LIKE 'procurement:invoice:%');
DELETE FROM permissions WHERE code LIKE 'procurement:invoice:%';
