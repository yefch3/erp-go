-- +goose Up
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('export:ownership:transfer', '转移单据负责人', 'export', '')
ON CONFLICT (code) DO NOTHING;

-- Deliberately not granted to SALES_RO. Reassigning a deal decides who gets
-- the credit for it, so it belongs to whoever manages the people, not to
-- whoever happens to hold the document: a salesperson who could transfer
-- their own work could also move a number off their own name.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('SUPER_ADMIN', 'SALES_MANAGER')
  AND p.code = 'export:ownership:transfer'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
  (SELECT id FROM permissions WHERE code = 'export:ownership:transfer');
DELETE FROM permissions WHERE code = 'export:ownership:transfer';
