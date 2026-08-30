-- +goose Up

-- 供应商对账页从只读的往来汇总，变成「员工在采购单上手填核销数字、手动
-- 确认完成」。读那一半沿用 00039 建的 procurement:recon:read，写是新的。
--
-- 不复用 procurement:payment:write：那对码管的是供应商付款页（建付款单、
-- 把一笔电汇拆到几张发票上）。一页一对码，前端 auth.can(...) 和网关
-- s.perm(...) 才逐字对得上——错开一个字，就是「看得见按钮、点下去 403」，
-- 或者更糟，反过来。
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('procurement:recon:write', '确认供应商核销', 'procurement', '/supplier-recon')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('SUPER_ADMIN', 'FINANCE')
  AND p.code IN ('procurement:recon:read', 'procurement:recon:write')
ON CONFLICT DO NOTHING;

-- 财务的采购单数据范围从 SELF 改成 ALL。
--
-- 00037 给所有非超管角色一律种了 SELF，那对采购专员是对的（只看自己下的
-- 单）。对财务是错的：**财务不是任何一张采购单的 buyer**，SELF 对它就等于
-- 零行。不改这一条，供应商对账页会打得开、表头和按钮都在、里面一张单都
-- 没有，而且全程没有一行报错——最难查的那种坏法。
--
-- 放宽的只是围栏，不是门：财务没有 procurement:order:read，采购订单页对它
-- 依然打不开。能多看见的是供应商付款页上同事登记的付款单——那本来就是
-- 财务该看全的东西。
INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT tenant_id, id, 'procurement_order', 'ALL'
FROM roles WHERE code = 'FINANCE'
ON CONFLICT (tenant_id, role_id, module)
DO UPDATE SET scope_type = 'ALL', custom_dept_ids = '{}';

-- +goose Down
UPDATE role_data_scopes SET scope_type = 'SELF'
 WHERE module = 'procurement_order'
   AND role_id IN (SELECT id FROM roles WHERE code = 'FINANCE');
DELETE FROM role_permissions
 WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'procurement:recon:write')
    OR (role_id IN (SELECT id FROM roles WHERE code = 'FINANCE')
        AND permission_id IN (SELECT id FROM permissions WHERE code = 'procurement:recon:read'));
DELETE FROM permissions WHERE code = 'procurement:recon:write';
