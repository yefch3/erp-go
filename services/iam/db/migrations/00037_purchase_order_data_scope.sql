-- +goose Up
-- Purchase orders carry the numbers a trading company actually guards:
-- supplier, price, payment terms. Until now any holder of
-- procurement:order:read saw every order in the tenant.
--
-- Same shape as 00031: administrators keep the company-wide view, everyone
-- else starts at SELF and is widened deliberately in the roles page. The
-- resolver already defaults an unconfigured module to SELF, so this seed is
-- about keeping admins working, not about closing the hole — the hole closes
-- the moment the service starts asking.
INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT tenant_id, id, 'procurement_order', CASE code
    WHEN 'SUPER_ADMIN' THEN 'ALL'
    ELSE 'SELF'
  END
FROM roles
ON CONFLICT (tenant_id, role_id, module) DO NOTHING;

-- +goose Down
DELETE FROM role_data_scopes WHERE module = 'procurement_order';
