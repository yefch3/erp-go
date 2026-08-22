-- +goose Up
-- Payments get their own permission for the same reason invoices did, one
-- step further along: registering what a factory claims and moving the
-- company's money are the two jobs segregation-of-duties exists to keep in
-- different hands.
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('procurement:payment:read',  '查看供应商付款', 'procurement', '/supplier-payments'),
  ('procurement:payment:write', '登记与核销付款', 'procurement', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN'
  AND p.code IN ('procurement:payment:read', 'procurement:payment:write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE code LIKE 'procurement:payment:%');
DELETE FROM permissions WHERE code LIKE 'procurement:payment:%';
