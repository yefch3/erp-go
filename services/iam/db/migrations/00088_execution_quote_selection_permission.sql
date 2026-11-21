-- +goose Up
INSERT INTO permissions (code, name, module, menu_path)
VALUES ('procurement:execution-quote:select', '选定工厂报价', 'procurement', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r
JOIN permissions p ON p.code IN ('procurement:execution-quote:select', 'procurement:requirement:read')
WHERE r.code IN ('PROCUREMENT_MANAGER', 'BOSS', 'SUPER_ADMIN')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'procurement:execution-quote:select');
DELETE FROM permissions WHERE code = 'procurement:execution-quote:select';
