-- +goose Up
-- 采购可在采购单内申请质检并处理异常，但当前业务不允许部分合格先发。
DELETE FROM role_permissions rp
USING roles r, permissions p
WHERE rp.role_id = r.id
  AND rp.tenant_id = r.tenant_id
  AND rp.permission_id = p.id
  AND p.code = 'quality:release:decide';

DELETE FROM permissions WHERE code = 'quality:release:decide';

-- +goose Down
INSERT INTO permissions(code, name, module, menu_path)
VALUES ('quality:release:decide', '决定部分合格先发数量', 'quality', '/quality/tasks')
ON CONFLICT(code) DO NOTHING;

INSERT INTO role_permissions(tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE p.code = 'quality:release:decide'
  AND r.code IN ('BUYER', 'PROCUREMENT_MANAGER', 'SUPER_ADMIN')
ON CONFLICT DO NOTHING;
