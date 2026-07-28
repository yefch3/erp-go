-- +goose Up
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('approval:flow:read',  '查看审批流配置', 'approval', '/settings/approvals'),
  ('approval:flow:write', '维护审批流配置', 'approval', '')
ON CONFLICT (code) DO NOTHING;

-- Designing approval flows is administrator work, not something every
-- approver should be able to do.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN' AND p.code IN ('approval:flow:read', 'approval:flow:write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE code IN ('approval:flow:read','approval:flow:write'));
DELETE FROM permissions WHERE code IN ('approval:flow:read','approval:flow:write');
