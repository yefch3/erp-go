-- +goose Up
-- 采购询价选择供应商后需要读取其合作工厂；只授予查看权限，不授予维护权限。
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.code IN ('BUYER', 'PROCUREMENT_MANAGER')
  AND p.code = 'masterdata:factory:read'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions rp
USING roles r, permissions p
WHERE rp.role_id = r.id
  AND rp.tenant_id = r.tenant_id
  AND rp.permission_id = p.id
  AND r.code IN ('BUYER', 'PROCUREMENT_MANAGER')
  AND p.code = 'masterdata:factory:read';
