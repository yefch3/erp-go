-- +goose Up
-- 采购结案（A3）自己的权限码。不搭 order:write 的车：结案是宣布「这单
-- 到此为止」的生命周期决定，和改单据是两种职责——把它做成单独的旋钮，
-- 管理员才能把「能建单」和「能结案」分给不同的人。种子只给 SUPER_ADMIN，
-- 其余角色由管理员在角色页显式打开（与 recon:read 同一策略）。
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('procurement:order:close', '采购订单结案', 'procurement', '/purchase-orders')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN' AND p.code = 'procurement:order:close'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE code = 'procurement:order:close');
DELETE FROM permissions WHERE code = 'procurement:order:close';
