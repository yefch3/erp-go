-- +goose Up

-- 有些数据库先跑到了较高版本，之后才合入较早编号的财务权限迁移；goose
-- 不会回头补跑较小编号。这里按当前角色定义做一次幂等补齐。
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN'
  AND p.code = 'procurement:recon:write'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.code = 'FINANCE'
  AND p.code IN (
    'export:contract:read', 'export:receipt:read', 'export:receipt:write',
    'export:shipment:read', 'procurement:payment:read',
    'procurement:payment:write', 'procurement:recon:read',
    'procurement:recon:write', 'fx:rate:read'
  )
ON CONFLICT DO NOTHING;

INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT tenant_id, id, 'procurement_order', 'ALL'
FROM roles
WHERE code = 'FINANCE'
ON CONFLICT (tenant_id, role_id, module)
DO UPDATE SET scope_type = 'ALL', custom_dept_ids = '{}';

-- +goose Down
-- 修复迁移不撤销权限：这些授权属于角色当前的正式定义。
SELECT 1;
