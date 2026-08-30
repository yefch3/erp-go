-- +goose Up
INSERT INTO permissions(code,name,module,menu_path) VALUES
 ('shipping:sourcing:read','查看售前船运询价','shipping','/shipping/sourcing'),
 ('shipping:sourcing:write','参与售前船运询价并录入报价','shipping',''),
 ('shipping:sourcing:approve','审核并提交船运经理统一方案','shipping','')
ON CONFLICT(code) DO NOTHING;

INSERT INTO roles(tenant_id,code,name,description)
SELECT DISTINCT tenant_id,'SHIPPING_MANAGER','船运经理','管理售前船运询价、主责人员和统一船运方案' FROM roles
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permissions(tenant_id,role_id,permission_id)
SELECT r.tenant_id,r.id,p.id FROM roles r CROSS JOIN permissions p
WHERE (r.code='LOGISTICS' AND p.code IN ('shipping:sourcing:read','shipping:sourcing:write'))
   OR (r.code='SHIPPING_MANAGER' AND p.code IN (
       'shipping:sourcing:read','shipping:sourcing:write','shipping:sourcing:approve',
       'shipping:schedule:read','shipping:schedule:write',
       'masterdata:supplier:read','masterdata:port:read','product:product:read'
   ))
   OR (r.code='SUPER_ADMIN' AND p.code LIKE 'shipping:sourcing:%')
ON CONFLICT DO NOTHING;

INSERT INTO role_data_scopes(tenant_id,role_id,module,scope_type)
SELECT tenant_id,id,'shipping','ALL' FROM roles WHERE code IN ('SHIPPING_MANAGER','LOGISTICS','SUPER_ADMIN')
ON CONFLICT(tenant_id,role_id,module) DO UPDATE SET scope_type='ALL',custom_dept_ids='{}';

INSERT INTO role_data_scopes(tenant_id,role_id,module,scope_type)
SELECT tenant_id,id,'procurement_sourcing','ALL' FROM roles WHERE code='SHIPPING_MANAGER'
ON CONFLICT(tenant_id,role_id,module) DO UPDATE SET scope_type='ALL',custom_dept_ids='{}';

-- +goose Down
DELETE FROM role_data_scopes WHERE module IN ('shipping','procurement_sourcing') AND role_id IN (SELECT id FROM roles WHERE code='SHIPPING_MANAGER');
DELETE FROM role_permissions WHERE role_id IN (SELECT id FROM roles WHERE code='SHIPPING_MANAGER')
 OR permission_id IN (SELECT id FROM permissions WHERE code LIKE 'shipping:sourcing:%');
DELETE FROM roles WHERE code='SHIPPING_MANAGER';
DELETE FROM permissions WHERE code LIKE 'shipping:sourcing:%';
