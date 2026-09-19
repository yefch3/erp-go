-- +goose Up
-- 修复“有操作权限但看不到单据”：这些岗位处理的是共享业务队列，单据属主
-- 通常不是当前用户。只把仍为系统旧默认 SELF 的范围升级为 ALL；管理员已经
-- 配置的部门、部门及下级、自定义或 ALL 范围保持原样。
WITH desired(role_code, module) AS (
  VALUES
    ('BUYER', 'procurement_requirement'),
    ('PROCUREMENT_MANAGER', 'procurement_requirement'),
    ('PROCUREMENT_MANAGER', 'procurement_order'),
    ('LOGISTICS', 'procurement_order')
)
INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type, custom_dept_ids)
SELECT r.tenant_id, r.id, d.module, 'ALL', '{}'
FROM roles r
JOIN desired d ON d.role_code = r.code
ON CONFLICT (tenant_id, role_id, module) DO UPDATE
SET scope_type = 'ALL', custom_dept_ids = '{}'
WHERE role_data_scopes.scope_type = 'SELF';

-- +goose Down
-- 数据范围可能在迁移后被管理员调整，自动回退会覆盖真实配置，因此不回写。
SELECT 1;
