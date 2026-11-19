-- +goose Up
INSERT INTO permissions (code, name, module, menu_path)
VALUES ('report:business:read', '查看经营报表', 'report', '/business-reports')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.code = 'BOSS' AND p.code = 'report:business:read'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'report:business:read');
DELETE FROM permissions WHERE code = 'report:business:read';
