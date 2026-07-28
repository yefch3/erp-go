-- +goose Up
-- Seeing where a document stands in approval is not the same as being able to
-- act on it: a salesperson needs to know their contract is waiting on the
-- sales manager, without any power to approve anything.
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('approval:instance:read', '查看审批进度', 'approval', '')
ON CONFLICT (code) DO NOTHING;

-- Everyone who can already do anything gets it; it reveals no more than the
-- document page it sits on.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE p.code = 'approval:instance:read'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'approval:instance:read');
DELETE FROM permissions WHERE code = 'approval:instance:read';
