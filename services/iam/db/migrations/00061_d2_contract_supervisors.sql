-- +goose Up
INSERT INTO role_permissions(tenant_id,role_id,permission_id)
SELECT r.tenant_id,r.id,p.id FROM roles r CROSS JOIN permissions p
WHERE r.code='BOSS' AND p.code IN ('export:quotation:read','export:contract:read','export:contract:approve','approval:task:act')
ON CONFLICT DO NOTHING;
INSERT INTO role_permissions(tenant_id,role_id,permission_id)
SELECT r.tenant_id,r.id,p.id FROM roles r CROSS JOIN permissions p
WHERE r.code='SALES_MANAGER' AND p.code='approval:task:act' ON CONFLICT DO NOTHING;
INSERT INTO role_data_scopes(tenant_id,role_id,module,scope_type)
SELECT tenant_id,id,'export','ALL' FROM roles WHERE code='BOSS'
ON CONFLICT(tenant_id,role_id,module) DO UPDATE SET scope_type='ALL';
-- +goose Down
-- Preserve explicit assignments and approval history.
SELECT 1;
