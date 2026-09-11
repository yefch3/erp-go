-- +goose Up
-- Assigned logistics approvers must be able to act on their pending tasks.
INSERT INTO role_permissions (tenant_id,role_id,permission_id)
SELECT r.tenant_id,r.id,p.id FROM roles r CROSS JOIN permissions p
WHERE r.code='SHIPPING_MANAGER' AND p.code='approval:task:act'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions rp USING roles r,permissions p
WHERE rp.role_id=r.id AND rp.tenant_id=r.tenant_id AND rp.permission_id=p.id
AND r.code='SHIPPING_MANAGER' AND p.code='approval:task:act';
