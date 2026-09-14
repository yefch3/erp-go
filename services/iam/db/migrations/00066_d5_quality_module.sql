-- +goose Up
INSERT INTO permissions(code,name,module,menu_path) VALUES
 ('quality:task:read','查看出厂前质检','quality','/quality/tasks'),
 ('quality:task:request','申请出厂前质检','quality','/purchase-orders'),
 ('quality:task:write','处理出厂前质检','quality','/quality/tasks'),
 ('quality:file:upload','保存质检资料','quality','/quality/tasks'),
 ('quality:release:decide','决定部分合格先发数量','quality','/quality/tasks')
ON CONFLICT(code) DO NOTHING;

INSERT INTO roles(tenant_id,code,name,description,status)
SELECT t.id,'QUALITY_INSPECTOR','质检专员','处理工厂出货前质检并保存每轮资料','ACTIVE'
FROM (
  SELECT id FROM tenants
  UNION
  -- 历史预置角色迁移直接为 tenant 1 建立对表基准；全新的一次性迁移库
  -- 尚无 tenants 行，也必须保留同一套基准，供启动播种与迁移做一致性校验。
  SELECT 1
) t ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permissions(tenant_id,role_id,permission_id)
SELECT r.tenant_id,r.id,p.id FROM roles r CROSS JOIN permissions p
WHERE (r.code='QUALITY_INSPECTOR' AND p.code IN ('quality:task:read','quality:task:write','quality:file:upload'))
   OR (r.code='SUPER_ADMIN' AND p.module='quality')
   OR (r.code IN ('BUYER','PROCUREMENT_MANAGER') AND p.code IN ('quality:task:read','quality:task:request','quality:release:decide'))
   OR (r.code IN ('LOGISTICS','SHIPPING_MANAGER','SALES','SALES_MANAGER','BOSS') AND p.code='quality:task:read')
ON CONFLICT DO NOTHING;

INSERT INTO role_data_scopes(tenant_id,role_id,module,scope_type,custom_dept_ids)
SELECT r.tenant_id,r.id,'quality',
 CASE WHEN r.code IN ('QUALITY_INSPECTOR','PROCUREMENT_MANAGER','LOGISTICS','SHIPPING_MANAGER','BOSS','SUPER_ADMIN') THEN 'ALL'
      WHEN r.code='SALES_MANAGER' THEN 'DEPT_AND_SUB' ELSE 'SELF' END,'{}'::bigint[]
FROM roles r WHERE r.code IN ('QUALITY_INSPECTOR','BUYER','PROCUREMENT_MANAGER','LOGISTICS','SHIPPING_MANAGER','SALES','SALES_MANAGER','BOSS','SUPER_ADMIN')
ON CONFLICT(tenant_id,role_id,module) DO UPDATE SET scope_type=EXCLUDED.scope_type,custom_dept_ids=EXCLUDED.custom_dept_ids;

-- +goose Down
DELETE FROM role_data_scopes WHERE module='quality';
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE module='quality');
DELETE FROM roles WHERE code='QUALITY_INSPECTOR';
DELETE FROM permissions WHERE module='quality';
