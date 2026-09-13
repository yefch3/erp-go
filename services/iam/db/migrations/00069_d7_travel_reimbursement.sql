-- +goose Up
INSERT INTO permissions(code,name,module,menu_path) VALUES
 ('procurement:reimbursement:manage','管理出差报销付款','procurement','/supplier-recon')
ON CONFLICT(code) DO NOTHING;

INSERT INTO roles(tenant_id,code,name,description,status)
SELECT t.id,'FINANCE_MANAGER','财务负责人','审批全公司报销并管理付款','ACTIVE'
FROM (SELECT id FROM tenants UNION SELECT 1) t
ON CONFLICT(tenant_id,code) DO NOTHING;

-- Finance leaders receive the finance role's operational permissions plus
-- the approval/payment authority specific to D7.
INSERT INTO role_permissions(tenant_id,role_id,permission_id)
SELECT fm.tenant_id,fm.id,rp.permission_id
FROM roles fm JOIN roles f ON f.tenant_id=fm.tenant_id AND f.code='FINANCE'
JOIN role_permissions rp ON rp.tenant_id=f.tenant_id AND rp.role_id=f.id
WHERE fm.code='FINANCE_MANAGER'
ON CONFLICT DO NOTHING;

INSERT INTO role_data_scopes(tenant_id,role_id,module,scope_type,custom_dept_ids)
SELECT fm.tenant_id,fm.id,s.module,s.scope_type,s.custom_dept_ids
FROM roles fm JOIN roles f ON f.tenant_id=fm.tenant_id AND f.code='FINANCE'
JOIN role_data_scopes s ON s.tenant_id=f.tenant_id AND s.role_id=f.id
WHERE fm.code='FINANCE_MANAGER'
ON CONFLICT(tenant_id,role_id,module) DO UPDATE
SET scope_type=excluded.scope_type,custom_dept_ids=excluded.custom_dept_ids;

INSERT INTO role_permissions(tenant_id,role_id,permission_id)
SELECT r.tenant_id,r.id,p.id FROM roles r CROSS JOIN permissions p
WHERE (r.code IN ('FINANCE_MANAGER','FINANCE','SUPER_ADMIN') AND p.code='procurement:reimbursement:manage')
   OR (r.code='FINANCE_MANAGER' AND p.code='approval:task:act')
ON CONFLICT DO NOTHING;

INSERT INTO role_data_scopes(tenant_id,role_id,module,scope_type,custom_dept_ids)
SELECT tenant_id,id,'procurement_order','ALL','{}'::bigint[] FROM roles WHERE code='FINANCE_MANAGER'
ON CONFLICT(tenant_id,role_id,module) DO UPDATE SET scope_type='ALL',custom_dept_ids='{}'::bigint[];

-- +goose Down
DELETE FROM role_data_scopes WHERE role_id IN (SELECT id FROM roles WHERE code='FINANCE_MANAGER');
DELETE FROM role_permissions WHERE role_id IN (SELECT id FROM roles WHERE code='FINANCE_MANAGER') OR permission_id IN (SELECT id FROM permissions WHERE code='procurement:reimbursement:manage');
DELETE FROM roles WHERE code='FINANCE_MANAGER';
DELETE FROM permissions WHERE code='procurement:reimbursement:manage';
