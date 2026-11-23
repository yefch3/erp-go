-- +goose Up
INSERT INTO permissions (code, name, module, menu_path)
VALUES ('report:business:read', '查看经理日报', 'report', '/manager/daily-report')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r JOIN permissions p ON p.code='report:business:read'
WHERE r.code IN ('SUPER_ADMIN','BOSS','SALES_MANAGER','PROCUREMENT_MANAGER','SHIPPING_MANAGER','FINANCE_MANAGER')
ON CONFLICT DO NOTHING;

INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT tenant_id, id, 'business_report',
       CASE WHEN code IN ('SUPER_ADMIN','BOSS') THEN 'ALL' ELSE 'DEPT_AND_SUB' END
FROM roles WHERE code IN ('SUPER_ADMIN','BOSS','SALES_MANAGER','PROCUREMENT_MANAGER','SHIPPING_MANAGER','FINANCE_MANAGER')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_data_scopes WHERE module='business_report';
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE code='report:business:read');
DELETE FROM permissions WHERE code='report:business:read';
