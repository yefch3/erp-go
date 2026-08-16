-- +goose Up
-- Existing administrators keep their company-wide view; every other role
-- starts least-privileged and may be widened explicitly in the roles page.
INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT tenant_id, id, 'procurement_sourcing', CASE code
    WHEN 'SUPER_ADMIN' THEN 'ALL'
    ELSE 'SELF'
  END
FROM roles
ON CONFLICT (tenant_id, role_id, module) DO NOTHING;

-- +goose Down
DELETE FROM role_data_scopes WHERE module = 'procurement_sourcing';
