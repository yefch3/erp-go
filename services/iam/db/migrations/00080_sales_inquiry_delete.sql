-- +goose Up

INSERT INTO permissions (code, name, module, menu_path)
VALUES ('sales:inquiry:delete', '删除客户询盘', 'sales', '')
ON CONFLICT (code) DO NOTHING;

-- 老板可以处理全公司的高风险操作；SUPER_ADMIN 由统一补全机制自动拥有全部权限。
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r
JOIN permissions p ON p.code = 'sales:inquiry:delete'
WHERE r.code = 'BOSS'
ON CONFLICT DO NOTHING;

-- +goose Down

DELETE FROM role_permissions rp USING permissions p
WHERE rp.permission_id = p.id AND p.code = 'sales:inquiry:delete';
DELETE FROM permissions WHERE code = 'sales:inquiry:delete';
