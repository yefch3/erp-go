-- +goose Up
-- 00025 曾在本机其他分支使用过；采用新版本号，避免 Goose 因已记录版本而跳过港口权限。
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('masterdata:port:read',  '查看港口', 'masterdata', '/basic/ports'),
  ('masterdata:port:write', '维护港口', 'masterdata', '')
ON CONFLICT (code) DO NOTHING;

-- 已有客户主数据权限的角色获得同级港口权限，升级后管理员菜单不会突然缺失。
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT DISTINCT rp.tenant_id, rp.role_id, target.id
FROM role_permissions rp
JOIN permissions source ON source.id = rp.permission_id
JOIN permissions target ON target.code = CASE source.code
  WHEN 'masterdata:customer:read' THEN 'masterdata:port:read'
  WHEN 'masterdata:customer:write' THEN 'masterdata:port:write'
END
WHERE source.code IN ('masterdata:customer:read', 'masterdata:customer:write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE code IN ('masterdata:port:read', 'masterdata:port:write'));
DELETE FROM permissions WHERE code IN ('masterdata:port:read', 'masterdata:port:write');
