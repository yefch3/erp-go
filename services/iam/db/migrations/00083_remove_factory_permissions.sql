-- +goose Up
DELETE FROM role_permissions
WHERE permission_id IN (
  SELECT id FROM permissions
  WHERE code IN ('masterdata:factory:read', 'masterdata:factory:write')
);
DELETE FROM permissions
WHERE code IN ('masterdata:factory:read', 'masterdata:factory:write');

-- +goose Down
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('masterdata:factory:read',  '查看工厂', 'masterdata', '/basic/suppliers/factories'),
  ('masterdata:factory:write', '维护工厂', 'masterdata', '')
ON CONFLICT (code) DO NOTHING;
