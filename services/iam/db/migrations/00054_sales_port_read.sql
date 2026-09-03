-- +goose Up
-- Sales records loading and discharge ports on existing paper contracts.
-- Read access supplies the searchable choices; free text remains available
-- when the paper contract uses a port that is not in master data.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.code IN ('SALES', 'SALES_MANAGER')
  AND p.code = 'masterdata:port:read'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions rp
USING roles r, permissions p
WHERE rp.role_id = r.id
  AND rp.tenant_id = r.tenant_id
  AND rp.permission_id = p.id
  AND r.code IN ('SALES', 'SALES_MANAGER')
  AND p.code = 'masterdata:port:read';
