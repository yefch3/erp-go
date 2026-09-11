-- +goose Up

-- A purchase-order approval links the approver to the order page. BOSS already
-- has approval permission, but needs read access and company-wide order scope.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.code = 'BOSS'
  AND p.code = 'procurement:order:read'
ON CONFLICT DO NOTHING;

INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT tenant_id, id, 'procurement_order', 'ALL'
FROM roles
WHERE code = 'BOSS'
ON CONFLICT (tenant_id, role_id, module)
DO UPDATE SET scope_type = 'ALL', custom_dept_ids = '{}';

-- +goose Down
UPDATE role_data_scopes
SET scope_type = 'SELF', custom_dept_ids = '{}'
WHERE module = 'procurement_order'
  AND role_id IN (SELECT id FROM roles WHERE code = 'BOSS');

DELETE FROM role_permissions
WHERE role_id IN (SELECT id FROM roles WHERE code = 'BOSS')
  AND permission_id IN (SELECT id FROM permissions WHERE code = 'procurement:order:read');
