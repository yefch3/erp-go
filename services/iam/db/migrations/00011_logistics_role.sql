-- +goose Up

-- Close the data-scope back door, and give the warehouse a role of its own.
--
-- The leak: purchase requirements carry the contract number and the customer
-- name, because a buyer needs to know what a shortage is for and when it is
-- due. Both sales roles had been granted procurement:requirement:read, so a
-- salesperson whose contract scope is SELF could read every other
-- salesperson's customers off the purchasing page — around the scope, not
-- through it.
--
-- The fix is the permission, not a data scope on procurement. Scoping
-- purchasing by document owner would break the one thing that makes the job
-- worth having: three contracts each short 500 have to become one order for
-- 1500, and a buyer who can only see their own cannot consolidate anything.
-- Purchasing and the warehouse are shared functions and read everything;
-- sales is a competitive position and reads its own.
DELETE FROM role_permissions
WHERE role_id IN (SELECT id FROM roles WHERE code IN ('SALES_RO', 'SALES_MANAGER'))
  AND permission_id IN (
      SELECT id FROM permissions
      WHERE code LIKE 'procurement:%'
         -- Sales must not receive or ship goods either. Reading stock stays:
         -- a salesperson has to see what is available before promising a
         -- delivery date, and the stock page carries no customer information.
         OR code = 'inventory:stock:write'
  );

-- 物流管理: receives purchases, ships contracts, counts what is on the shelf.
--
-- It gets procurement:order:read but not :write on purpose — the warehouse
-- confirms what arrived against an order, it does not decide what to buy.
INSERT INTO roles (tenant_id, code, name, description) VALUES
  (1, 'LOGISTICS', '物流管理', '仓储与发运：入库、出库、采购收货、库存查询')
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'LOGISTICS'
  AND p.code IN (
      'inventory:stock:read',
      'inventory:stock:write',
      -- Needed to open the order a delivery is booked against.
      'procurement:order:read',
      -- Reference data every warehouse form needs.
      'product:product:read',
      'masterdata:supplier:read'
  )
ON CONFLICT DO NOTHING;

-- Warehouse work is not scoped by who sold the goods: a picker has to see
-- every outstanding shipment or the warehouse cannot function.
INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT 1, r.id, 'export', 'ALL' FROM roles r WHERE r.code = 'LOGISTICS'
ON CONFLICT (tenant_id, role_id, module) DO NOTHING;

-- +goose Down
DELETE FROM role_data_scopes WHERE role_id IN (SELECT id FROM roles WHERE code = 'LOGISTICS');
DELETE FROM role_permissions WHERE role_id IN (SELECT id FROM roles WHERE code = 'LOGISTICS');
DELETE FROM roles WHERE tenant_id = 1 AND code = 'LOGISTICS';

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('SALES_RO', 'SALES_MANAGER')
  AND (p.code LIKE 'procurement:requirement:%' OR p.code = 'inventory:stock:write')
ON CONFLICT DO NOTHING;
