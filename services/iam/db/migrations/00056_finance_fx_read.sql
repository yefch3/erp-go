-- +goose Up

-- 财务菜单固定以银行流水、入账、出账、汇率收尾。财务只需要查看系统汇率，
-- 不在这里获得维护汇率的权限。
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.code = 'FINANCE'
  AND p.code = 'fx:rate:read'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions
WHERE role_id IN (SELECT id FROM roles WHERE code = 'FINANCE')
  AND permission_id IN (SELECT id FROM permissions WHERE code = 'fx:rate:read');
