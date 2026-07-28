-- +goose Up
-- Contracts are now filtered by data scope. Without a row here the resolver
-- falls back to SELF, which would silently hide every existing contract from
-- the administrator, so the defaults are seeded explicitly.
INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT 1, id, 'export', CASE code
    WHEN 'SUPER_ADMIN'   THEN 'ALL'
    WHEN 'SALES_MANAGER' THEN 'ALL'
    ELSE 'SELF'
  END
FROM roles WHERE tenant_id = 1
ON CONFLICT (tenant_id, role_id, module) DO NOTHING;

-- +goose Down
DELETE FROM role_data_scopes WHERE tenant_id = 1 AND module = 'export';
