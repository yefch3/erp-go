-- +goose Up
INSERT INTO permissions(code,name,module)
VALUES ('fx:rate:write','确认有效汇率','fx') ON CONFLICT(code) DO NOTHING;
INSERT INTO role_permissions(tenant_id,role_id,permission_id)
SELECT r.tenant_id,r.id,p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('ADMIN','FINANCE') AND p.code='fx:rate:write'
ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE code='fx:rate:write');
DELETE FROM permissions WHERE code='fx:rate:write';
