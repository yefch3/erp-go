-- +goose Up
-- Sales may create customers and maintain their customer information.
-- Use each role's tenant and preserve all other customized permissions.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('SALES', 'SALES_MANAGER')
  AND p.code = 'masterdata:customer:write'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions rp USING roles r, permissions p
WHERE rp.tenant_id = r.tenant_id AND rp.role_id = r.id
  AND rp.permission_id = p.id
  AND r.code IN ('SALES', 'SALES_MANAGER')
  AND p.code = 'masterdata:customer:write';