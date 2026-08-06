-- +goose Up

-- D2 creates the capability boundaries now. Employee/department data scope
-- assignments are intentionally deferred to D4, so only SUPER_ADMIN receives
-- these permissions by default.
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('shipping:document:view',       '查看船期单证', 'shipping', ''),
  ('shipping:document:download',   '下载船期单证', 'shipping', ''),
  ('shipping:document:upload',     '上传船期单证', 'shipping', ''),
  ('shipping:document:invalidate', '作废船期单证', 'shipping', ''),
  ('shipping:document:manage',     '管理全部船期单证', 'shipping', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN'
  AND p.code IN (
    'shipping:document:view', 'shipping:document:download',
    'shipping:document:upload', 'shipping:document:invalidate',
    'shipping:document:manage'
  )
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (
  SELECT id FROM permissions WHERE code IN (
    'shipping:document:view', 'shipping:document:download',
    'shipping:document:upload', 'shipping:document:invalidate',
    'shipping:document:manage'
  ));
DELETE FROM permissions WHERE code IN (
  'shipping:document:view', 'shipping:document:download',
  'shipping:document:upload', 'shipping:document:invalidate',
  'shipping:document:manage'
);
