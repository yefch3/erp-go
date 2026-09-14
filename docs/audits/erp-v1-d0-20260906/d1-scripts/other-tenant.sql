INSERT INTO tenants(id,name) VALUES(22001,'D1 other tenant') ON CONFLICT DO NOTHING;
INSERT INTO departments(tenant_id,code,name) VALUES(22001,'D1','D1') ON CONFLICT DO NOTHING;
INSERT INTO employees(tenant_id,code,name,department_id,email,email_verified_at)
SELECT 22001,'D1-X','D1 X',id,'x@d1-other.example.test',now() FROM departments WHERE tenant_id=22001 AND code='D1' ON CONFLICT DO NOTHING;
INSERT INTO users(tenant_id,employee_id,username,password_hash)
SELECT 22001,e.id,e.email,u.password_hash FROM employees e CROSS JOIN users u JOIN employees s ON s.id=u.employee_id WHERE e.tenant_id=22001 AND e.code='D1-X' AND s.tenant_id=1 AND s.code='D1-S2' ON CONFLICT DO NOTHING;
INSERT INTO roles(tenant_id,code,name) VALUES(22001,'SALES','Sales') ON CONFLICT DO NOTHING;
INSERT INTO role_permissions(tenant_id,role_id,permission_id)
SELECT 22001,r.id,p.permission_id FROM roles r CROSS JOIN role_permissions p JOIN roles s ON s.id=p.role_id WHERE r.tenant_id=22001 AND r.code='SALES' AND s.tenant_id=1 AND s.code='SALES' ON CONFLICT DO NOTHING;
INSERT INTO employee_roles(tenant_id,employee_id,role_id)
SELECT 22001,e.id,r.id FROM employees e JOIN roles r ON r.tenant_id=e.tenant_id WHERE e.tenant_id=22001 AND e.code='D1-X' AND r.code='SALES' ON CONFLICT DO NOTHING;
INSERT INTO role_data_scopes(tenant_id,role_id,module,scope_type)
SELECT 22001,r.id,'procurement_sourcing','SELF' FROM roles r WHERE r.tenant_id=22001 AND r.code='SALES' ON CONFLICT DO NOTHING;
