-- +goose Up
-- Buyers request an inspection from the purchase-order page. This permission
-- intentionally does not open the quality workbench, but it is unusable
-- without permission to read the purchase order that owns the request.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT rp.tenant_id, rp.role_id, required_permission.id
FROM role_permissions rp
JOIN permissions action_permission
  ON action_permission.id = rp.permission_id
 AND action_permission.code = 'quality:task:request'
JOIN permissions required_permission
  ON required_permission.code = 'procurement:order:read'
ON CONFLICT DO NOTHING;

-- +goose Down
-- The read permission may also have been granted directly, so it is not safe
-- to remove automatically.
SELECT 1;
