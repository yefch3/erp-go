-- +goose Up

-- The shipping document gets its own permission pair rather than riding on
-- export:contract:*. Booking a boat and writing a contract are different jobs
-- done by different people: the forwarder desk types bills of lading all day
-- and must never be able to alter a price, while a salesperson has to see
-- where their customer's goods are without being able to declare them sailed.
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('export:shipment:read',  '查看出运单', 'export', '/export/shipments'),
  ('export:shipment:write', '维护出运单', 'export', '')
ON CONFLICT (code) DO NOTHING;

-- Logistics books and confirms.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('SUPER_ADMIN', 'LOGISTICS')
  AND p.code IN ('export:shipment:read', 'export:shipment:write')
ON CONFLICT DO NOTHING;

-- Sales reads. "Which boat is my customer's order on" is the single most
-- asked question on an export desk, and making them ring the forwarder for it
-- is how the answer ends up in somebody's private spreadsheet.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('SALES_RO', 'SALES_MANAGER')
  AND p.code = 'export:shipment:read'
ON CONFLICT DO NOTHING;

-- Logistics needs to read contracts to load one onto a boat: the line being
-- shipped, its product and its unit come off the contract's in-force version.
-- Read only — the export data scope already grants ALL, and a warehouse that
-- could edit contract terms would be a much larger hole than the one closed
-- in 00011.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'LOGISTICS' AND p.code = 'export:contract:read'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (
  SELECT id FROM permissions WHERE code IN ('export:shipment:read', 'export:shipment:write'));
DELETE FROM role_permissions
WHERE role_id IN (SELECT id FROM roles WHERE code = 'LOGISTICS')
  AND permission_id IN (SELECT id FROM permissions WHERE code = 'export:contract:read');
DELETE FROM permissions WHERE code IN ('export:shipment:read', 'export:shipment:write');
