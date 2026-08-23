-- +goose Up
-- 采购需求的数据范围（A1 收尾）。属主是合同负责人（手工需求是创建人），
-- 所以 SELF 对销售意味着「只看自己客户的采购动向」。采购员的活是把
-- 整个待采购池子合并下单，按人切池子会把活干断——所以采购类角色要在
-- 角色页把本模块显式开到 ALL。种子照旧只保管理员不断电，其余 SELF，
-- 解析器对未配置模块本就兜底 SELF（fail-closed）。
INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT tenant_id, id, 'procurement_requirement', CASE code
    WHEN 'SUPER_ADMIN' THEN 'ALL'
    ELSE 'SELF'
  END
FROM roles
ON CONFLICT (tenant_id, role_id, module) DO NOTHING;

-- +goose Down
DELETE FROM role_data_scopes WHERE module = 'procurement_requirement';
