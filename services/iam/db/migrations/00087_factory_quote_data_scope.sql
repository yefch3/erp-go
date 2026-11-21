-- +goose Up
INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT tenant_id, id, 'procurement_factory_quote',
       CASE WHEN code = 'BUYER' THEN 'SELF' ELSE 'ALL' END
FROM roles
WHERE code IN ('BUYER', 'PROCUREMENT_MANAGER', 'BOSS', 'SUPER_ADMIN')
ON CONFLICT (tenant_id, role_id, module) DO NOTHING;

-- +goose Down
DELETE FROM role_data_scopes WHERE module = 'procurement_factory_quote';
