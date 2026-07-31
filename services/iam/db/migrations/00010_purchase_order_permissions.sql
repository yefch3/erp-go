-- +goose Up

-- Purchase orders get their own permission rather than riding on the
-- requirement one.
--
-- Reading what has to be bought and committing the company's money to a
-- supplier are different jobs, and in most companies different people: a
-- planner may raise requirements all day without ever being allowed to place
-- an order. Folding them together would make the approval flow the only
-- control on spending, and an approval nobody is required to seek is not a
-- control at all.
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('procurement:order:read',  '查看采购单', 'procurement', '/purchase-orders'),
  ('procurement:order:write', '维护采购单', 'procurement', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN'
  AND p.code IN ('procurement:order:read', 'procurement:order:write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE code LIKE 'procurement:order:%');
DELETE FROM permissions WHERE code LIKE 'procurement:order:%';
