-- +goose Up
-- Payments get their own permission for the same reason invoices did, one
-- step further along: registering what a factory claims and moving the
-- company's money are the two jobs segregation-of-duties exists to keep in
-- different hands.
--
-- The two codes already exist from 00033 (as 查看/维护采购付款), so the
-- first INSERT is a deliberate no-op; what this migration adds is the
-- SUPER_ADMIN grant, for every tenant's SUPER_ADMIN — the role is seeded
-- per tenant at bootstrap, so the grant must follow the role's tenant, not
-- a hardcoded one.
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('procurement:payment:read',  '查看供应商付款', 'procurement', '/supplier-payments'),
  ('procurement:payment:write', '登记与核销付款', 'procurement', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN'
  AND p.code IN ('procurement:payment:read', 'procurement:payment:write')
ON CONFLICT DO NOTHING;

-- +goose Down
-- Only the grant this migration made is rolled back, and only from
-- SUPER_ADMIN: the permission rows themselves belong to 00033, and the
-- grants 00033 gave other roles are not this migration's to delete.
DELETE FROM role_permissions rp
USING roles r, permissions p
WHERE rp.role_id = r.id AND rp.permission_id = p.id
  AND r.code = 'SUPER_ADMIN'
  AND p.code IN ('procurement:payment:read', 'procurement:payment:write');
