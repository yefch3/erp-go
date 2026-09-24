-- +goose Up
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('procurement:daily-price:read', '查看每日基价和趋势', 'procurement', '/procurement/daily-prices'),
  ('procurement:daily-price:write', '填写每日基价和基差', 'procurement', ''),
  ('procurement:daily-price:manage', '管理每日基价配置及全部记录', 'procurement', ''),
  ('procurement:daily-price:delete', '删除错误的每日基价记录', 'procurement', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id FROM roles r JOIN permissions p ON p.code IN
  ('procurement:daily-price:read','procurement:daily-price:write','procurement:daily-price:manage','procurement:daily-price:delete')
WHERE (r.code='BUYER' AND p.code IN ('procurement:daily-price:read','procurement:daily-price:write'))
   OR (r.code='PROCUREMENT_MANAGER' AND p.code <> 'procurement:daily-price:delete')
   OR r.code='SUPER_ADMIN'
ON CONFLICT DO NOTHING;

INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type, custom_dept_ids)
SELECT tenant_id, id, 'daily_base_price', CASE WHEN code='BUYER' THEN 'SELF' ELSE 'ALL' END, '{}'
FROM roles WHERE code IN ('BUYER','PROCUREMENT_MANAGER','SUPER_ADMIN')
ON CONFLICT (tenant_id,role_id,module) DO NOTHING;

-- +goose Down
DELETE FROM role_data_scopes WHERE module='daily_base_price';
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE code LIKE 'procurement:daily-price:%');
DELETE FROM permissions WHERE code LIKE 'procurement:daily-price:%';
