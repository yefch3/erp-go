-- +goose Up
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('procurement:requirement:read',  '查看采购需求', 'procurement', '/requirements'),
  ('procurement:requirement:write', '维护采购需求', 'procurement', '')
ON CONFLICT (code) DO NOTHING;

-- Requirements come from contracts the company has already signed, so seeing
-- them is not sensitive in the way a price list is. Acting on them is
-- purchasing work; for now only the administrator has it, until there is a
-- buyer role to give it to.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN'
  AND p.code IN ('procurement:requirement:read', 'procurement:requirement:write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE code LIKE 'procurement:requirement:%');
DELETE FROM permissions WHERE code LIKE 'procurement:requirement:%';
