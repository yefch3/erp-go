-- +goose Up

-- Collection gets its own permission pair. Reconciling bank lines is finance
-- work, not sales work: the person who decides which contract a wire paid for
-- is not the person who sold it, and letting either do the other's job by
-- accident is how a payment ends up on the wrong customer's account.
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('export:receipt:read',  '查看收款对账', 'export', '/export/receipts'),
  ('export:receipt:write', '登记与核销收款', 'export', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN'
  AND p.code IN ('export:receipt:read', 'export:receipt:write')
ON CONFLICT DO NOTHING;

-- 财务: records what the bank said and decides what it paid for. No contract
-- write, no shipment write — finance reconciles, it does not sell or ship.
INSERT INTO roles (tenant_id, code, name, description) VALUES
  (1, 'FINANCE', '财务', '收款登记与合同核销、银行流水对账')
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'FINANCE'
  AND p.code IN (
      'export:receipt:read',
      'export:receipt:write',
      -- Needed to see which contract a payment belongs to, and for how much.
      'export:contract:read',
      -- Which boat the goods went on answers "have we earned this money yet".
      'export:shipment:read',
      'masterdata:customer:read'
  )
ON CONFLICT DO NOTHING;

-- Reconciliation is not scoped by who sold the contract: a payment can cover
-- several salespeople's deals at once, and finance has to see all of them or
-- it cannot allocate the line at all.
INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT 1, r.id, 'export', 'ALL' FROM roles r WHERE r.code = 'FINANCE'
ON CONFLICT (tenant_id, role_id, module) DO NOTHING;

-- Sales reads its own collection position through the contract page, which is
-- gated on export:contract:read — no receipt permission needed for that.

-- +goose Down
DELETE FROM role_data_scopes WHERE role_id IN (SELECT id FROM roles WHERE code = 'FINANCE');
DELETE FROM role_permissions WHERE role_id IN (SELECT id FROM roles WHERE code = 'FINANCE');
DELETE FROM roles WHERE tenant_id = 1 AND code = 'FINANCE';
DELETE FROM role_permissions WHERE permission_id IN (
  SELECT id FROM permissions WHERE code IN ('export:receipt:read', 'export:receipt:write'));
DELETE FROM permissions WHERE code IN ('export:receipt:read', 'export:receipt:write');
