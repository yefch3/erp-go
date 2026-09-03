-- +goose Up
-- Sales must select the original buyer and supplier while taking over an
-- already-running paper contract. These are read-only lookup permissions.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.code IN ('iam:employee:read', 'masterdata:supplier:read')
WHERE r.code IN ('SALES', 'SALES_MANAGER')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions rp
USING roles r, permissions p
WHERE rp.role_id = r.id AND rp.permission_id = p.id
  AND r.code IN ('SALES', 'SALES_MANAGER')
  AND p.code IN ('iam:employee:read', 'masterdata:supplier:read');
