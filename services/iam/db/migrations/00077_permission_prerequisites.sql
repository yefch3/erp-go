-- +goose Up
-- 有操作权限却没有页面读取权限时，用户会看到“管理员已经勾选，但菜单和按钮
-- 仍然不存在”。把操作所依赖的读取能力补齐；只增不减，不改变任何数据范围。
WITH required(action_code, required_code) AS (
  VALUES
    ('approval:flow:write', 'approval:flow:read'),
    ('export:contract:approve', 'export:contract:read'),
    ('export:contract:write', 'export:contract:read'),
    ('export:ownership:transfer', 'export:contract:read'),
    ('export:quotation:write', 'export:quotation:read'),
    ('export:receipt:write', 'export:receipt:read'),
    ('export:shipment:write', 'export:shipment:read'),
    ('fx:rate:write', 'fx:rate:read'),
    ('iam:department:write', 'iam:department:read'),
    ('iam:employee:write', 'iam:employee:read'),
    ('iam:role:write', 'iam:role:read'),
    ('inventory:stock:cost', 'inventory:stock:read'),
    ('inventory:stock:import', 'inventory:stock:read'),
    ('inventory:stock:write', 'inventory:stock:read'),
    ('mail:email:export', 'mail:email:read'),
    ('mail:email:write', 'mail:email:read'),
    ('mail:supervision:audit', 'mail:supervision:read'),
    ('mail:suppression:write', 'mail:email:read'),
    ('masterdata:customer:write', 'masterdata:customer:read'),
    ('masterdata:factory:write', 'masterdata:factory:read'),
    ('masterdata:port:write', 'masterdata:port:read'),
    ('masterdata:supplier:write', 'masterdata:supplier:read'),
    ('procurement:exception:write', 'procurement:order:read'),
    ('procurement:invoice:write', 'procurement:invoice:read'),
    ('procurement:order:cancel', 'procurement:order:read'),
    ('procurement:order:close', 'procurement:order:read'),
    ('procurement:order:send', 'procurement:order:read'),
    ('procurement:order:submit', 'procurement:order:read'),
    ('procurement:order:write', 'procurement:order:read'),
    ('procurement:payment:write', 'procurement:payment:read'),
    ('procurement:production:write', 'procurement:order:read'),
    ('procurement:receipt:write', 'procurement:order:read'),
    ('procurement:recon:write', 'procurement:recon:read'),
    ('procurement:recon:write', 'procurement:order:read'),
    ('procurement:requirement:exception', 'procurement:requirement:read'),
    ('procurement:requirement:write', 'procurement:requirement:read'),
    ('procurement:sourcing:approve', 'procurement:sourcing:read'),
    ('procurement:sourcing:price', 'procurement:sourcing:read'),
    ('procurement:sourcing:send', 'procurement:sourcing:read'),
    ('procurement:sourcing:write', 'procurement:sourcing:read'),
    ('product:product:write', 'product:product:read'),
    ('quality:file:upload', 'quality:task:read'),
    ('quality:task:write', 'quality:task:read'),
    ('sales:inquiry:submit', 'sales:inquiry:read'),
    ('sales:inquiry:write', 'sales:inquiry:read'),
    ('shipping:document:download', 'shipping:document:view'),
    ('shipping:document:download', 'shipping:schedule:read'),
    ('shipping:document:invalidate', 'shipping:document:view'),
    ('shipping:document:invalidate', 'shipping:schedule:read'),
    ('shipping:document:manage', 'shipping:document:view'),
    ('shipping:document:manage', 'shipping:schedule:read'),
    ('shipping:document:upload', 'shipping:document:view'),
    ('shipping:document:upload', 'shipping:schedule:read'),
    ('shipping:document:view', 'shipping:schedule:read'),
    ('shipping:progress:write', 'shipping:schedule:read'),
    ('shipping:route:write', 'shipping:schedule:read'),
    ('shipping:schedule:write', 'shipping:schedule:read'),
    ('shipping:sourcing:approve', 'shipping:sourcing:read'),
    ('shipping:sourcing:write', 'shipping:sourcing:read')
)
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT rp.tenant_id, rp.role_id, required_permission.id
FROM role_permissions rp
JOIN permissions action_permission ON action_permission.id = rp.permission_id
JOIN required ON required.action_code = action_permission.code
JOIN permissions required_permission ON required_permission.code = required.required_code
ON CONFLICT DO NOTHING;

-- +goose Down
-- 这些读取权限也可能在迁移后由管理员明确授予，回退时无法区分来源，因此不删。
SELECT 1;
