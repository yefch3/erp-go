-- +goose Up
-- The reconciliation view gets its own permission rather than riding on
-- invoice:read or payment:read: it aggregates both plus orders, so either
-- existing knob alone would leak the other's numbers. A separate switch
-- lets the admin hand "sees the whole book" to finance explicitly.
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('procurement:recon:read', '查看供应商对账', 'procurement', '/supplier-statements')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN' AND p.code = 'procurement:recon:read'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE code = 'procurement:recon:read');
DELETE FROM permissions WHERE code = 'procurement:recon:read';
