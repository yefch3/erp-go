-- +goose Up
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('masterdata:factory:read',  '查看工厂', 'masterdata', '/basic/suppliers/factories'),
  ('masterdata:factory:write', '维护工厂', 'masterdata', '')
ON CONFLICT (code) DO NOTHING;

-- 新权限只默认授予超级管理员；普通角色必须在角色权限页面明确配置。
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN'
  AND p.code IN ('masterdata:factory:read', 'masterdata:factory:write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE code IN ('masterdata:factory:read', 'masterdata:factory:write'));
DELETE FROM permissions WHERE code IN ('masterdata:factory:read', 'masterdata:factory:write');
