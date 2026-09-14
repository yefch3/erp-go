-- +goose Up
-- Main applied username login as version 59; rebuild applied the D1 boss
-- inquiry grant as version 59. Replay the idempotent grant after both histories
-- so an upgrade from either baseline has both capabilities.
INSERT INTO roles(tenant_id,code,name,description)
SELECT tenant_id,'BOSS','老板','查看本公司客户询盘和已提交源报价' FROM (SELECT id AS tenant_id FROM tenants UNION SELECT tenant_id FROM roles) t
ON CONFLICT(tenant_id,code) DO NOTHING;
INSERT INTO role_permissions(tenant_id,role_id,permission_id)
SELECT r.tenant_id,r.id,p.id FROM roles r CROSS JOIN permissions p
WHERE r.code='BOSS' AND p.code='sales:inquiry:read' ON CONFLICT DO NOTHING;
INSERT INTO role_data_scopes(tenant_id,role_id,module,scope_type)
SELECT tenant_id,id,'procurement_sourcing','ALL' FROM roles WHERE code='BOSS'
ON CONFLICT(tenant_id,role_id,module) DO NOTHING;

-- +goose Down
-- Preserve grants which may predate this reconciliation.
SELECT 1;
