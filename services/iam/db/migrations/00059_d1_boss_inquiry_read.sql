-- +goose Up
-- D1 grants the boss read access to inquiries and received quotes only.
-- Contract decisions remain for the explicitly scoped later implementation.
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
-- Preserve explicit role assignments and permissions on rollback.
SELECT 1;
