-- +goose Up
-- D6: finance prepares payment from ETA changes and therefore needs the same
-- read-only schedule, attachment and freight snapshot used by logistics.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r
JOIN permissions p ON p.code IN (
  'shipping:schedule:read', 'shipping:document:view', 'shipping:document:download'
)
WHERE r.code = 'FINANCE'
ON CONFLICT DO NOTHING;

INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT tenant_id, id, 'shipping', 'ALL'
FROM roles
WHERE code = 'FINANCE'
ON CONFLICT (tenant_id, role_id, module) DO UPDATE SET scope_type = EXCLUDED.scope_type;

-- Formal schedule attachments are maintained by logistics. The document
-- permissions existed before D6 but were only seeded to SUPER_ADMIN, which
-- left the logistics upload panel unusable.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r
JOIN permissions p ON p.code IN (
  'shipping:document:view', 'shipping:document:download',
  'shipping:document:upload', 'shipping:document:invalidate'
)
WHERE r.code = 'LOGISTICS'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_data_scopes
WHERE module = 'shipping' AND role_id IN (SELECT id FROM roles WHERE code = 'FINANCE');

DELETE FROM role_permissions
WHERE (role_id IN (SELECT id FROM roles WHERE code = 'FINANCE')
       AND permission_id IN (SELECT id FROM permissions WHERE code IN (
         'shipping:schedule:read', 'shipping:document:view', 'shipping:document:download'
       )))
   OR (role_id IN (SELECT id FROM roles WHERE code = 'LOGISTICS')
       AND permission_id IN (SELECT id FROM permissions WHERE code IN (
         'shipping:document:view', 'shipping:document:download',
         'shipping:document:upload', 'shipping:document:invalidate'
       )));
