-- +goose Up
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.code = 'BOSS' AND p.code IN (
  'masterdata:supplier:read', 'procurement:requirement:read',
  'procurement:sourcing:read', 'procurement:sourcing:write',
  'procurement:sourcing:price', 'procurement:sourcing:approve',
  'procurement:order:write', 'procurement:order:submit'
)
ON CONFLICT DO NOTHING;

INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT tenant_id, id, 'procurement_requirement', 'ALL' FROM roles WHERE code = 'BOSS'
ON CONFLICT (tenant_id, role_id, module) DO UPDATE SET scope_type = EXCLUDED.scope_type;

-- +goose Down
-- Existing administrator grants are retained.
