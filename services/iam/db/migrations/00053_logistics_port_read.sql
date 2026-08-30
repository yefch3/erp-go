-- +goose Up
-- 售前船运询价录入正式报价时，物流人员需要从启用港口中选择起运港和目的港。
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.code = 'LOGISTICS'
  AND p.code = 'masterdata:port:read'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions rp
USING roles r, permissions p
WHERE rp.role_id = r.id
  AND rp.tenant_id = r.tenant_id
  AND rp.permission_id = p.id
  AND r.code = 'LOGISTICS'
  AND p.code = 'masterdata:port:read';
