-- +goose Up

-- 长期运行的数据库可能已经越过 00050，导致后合入的较小编号迁移不再执行。
-- 先补权限字典，再给当前需要录入付款的两个角色授权。
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('procurement:recon:write', '添加和维护出账', 'procurement', '/supplier-recon')
ON CONFLICT (code) DO UPDATE
SET name = EXCLUDED.name,
    module = EXCLUDED.module,
    menu_path = EXCLUDED.menu_path;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.code IN ('SUPER_ADMIN', 'FINANCE')
  AND p.code = 'procurement:recon:write'
ON CONFLICT DO NOTHING;

-- +goose Down
-- 修复迁移不撤销权限：该权限属于两个角色当前的正式定义。
SELECT 1;
