-- +goose Up
-- 质检任务页、现场资料和历史仅向质检部门开放。采购仍可从采购单申请
-- 质检并保留部分合格放行职责，但不再拥有质检任务读取权限。
DELETE FROM role_permissions rp
USING roles r, permissions p
WHERE rp.role_id=r.id
  AND rp.tenant_id=r.tenant_id
  AND rp.permission_id=p.id
  AND p.code='quality:task:read'
  AND r.code IN ('BUYER','PROCUREMENT_MANAGER','LOGISTICS','SHIPPING_MANAGER','SALES','SALES_MANAGER','BOSS');

DELETE FROM role_data_scopes s
USING roles r
WHERE s.role_id=r.id
  AND s.tenant_id=r.tenant_id
  AND s.module='quality'
  AND r.code IN ('LOGISTICS','SHIPPING_MANAGER','SALES','SALES_MANAGER','BOSS');

-- +goose Down
INSERT INTO role_permissions(tenant_id,role_id,permission_id)
SELECT r.tenant_id,r.id,p.id
FROM roles r CROSS JOIN permissions p
WHERE p.code='quality:task:read'
  AND r.code IN ('BUYER','PROCUREMENT_MANAGER','LOGISTICS','SHIPPING_MANAGER','SALES','SALES_MANAGER','BOSS')
ON CONFLICT DO NOTHING;

INSERT INTO role_data_scopes(tenant_id,role_id,module,scope_type,custom_dept_ids)
SELECT r.tenant_id,r.id,'quality',
 CASE WHEN r.code IN ('PROCUREMENT_MANAGER','LOGISTICS','SHIPPING_MANAGER','BOSS') THEN 'ALL'
      WHEN r.code='SALES_MANAGER' THEN 'DEPT_AND_SUB' ELSE 'SELF' END,
 '{}'::bigint[]
FROM roles r
WHERE r.code IN ('BUYER','PROCUREMENT_MANAGER','LOGISTICS','SHIPPING_MANAGER','SALES','SALES_MANAGER','BOSS')
ON CONFLICT(tenant_id,role_id,module) DO UPDATE
SET scope_type=EXCLUDED.scope_type,custom_dept_ids=EXCLUDED.custom_dept_ids;
