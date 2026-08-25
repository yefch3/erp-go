-- Quantities cross this boundary as text, same rule as everywhere else in
-- this system: Go holds decimal strings and never a float64.

-- name: ListWarehouses :many
SELECT id, code, name, wh_type, address, manager_id, status, created_at,
       profile_type, country_code, city, timezone, accounting_mode, updated_at
FROM warehouses
WHERE tenant_id = $1 AND (sqlc.arg(include_inactive)::bool OR status = 'ACTIVE')
ORDER BY code;

-- name: GetWarehouse :one
SELECT id, code, name, wh_type, address, manager_id, status, created_at,
       profile_type, country_code, city, timezone, accounting_mode, updated_at
FROM warehouses
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: CreateWarehouse :one
INSERT INTO warehouses (
    tenant_id, code, name, wh_type, address, status, profile_type,
    country_code, city, timezone, accounting_mode
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(code)::text, sqlc.arg(name)::text,
    sqlc.arg(wh_type)::text, sqlc.arg(address)::text, sqlc.arg(status)::text,
    sqlc.arg(profile_type)::text, sqlc.arg(country_code)::text,
    sqlc.arg(city)::text, sqlc.arg(timezone)::text, sqlc.arg(accounting_mode)::text
)
RETURNING id;

-- name: UpdateWarehouse :exec
UPDATE warehouses SET
    code = sqlc.arg(code)::text,
    name = sqlc.arg(name)::text,
    wh_type = sqlc.arg(wh_type)::text,
    address = sqlc.arg(address)::text,
    status = sqlc.arg(status)::text,
    profile_type = sqlc.arg(profile_type)::text,
    country_code = sqlc.arg(country_code)::text,
    city = sqlc.arg(city)::text,
    timezone = sqlc.arg(timezone)::text,
    accounting_mode = sqlc.arg(accounting_mode)::text,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: ListWarehouseContacts :many
SELECT id, contact_type, employee_id, name, phone, email, is_primary, status
FROM warehouse_contacts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND warehouse_id = sqlc.arg(warehouse_id)::bigint
ORDER BY contact_type, is_primary DESC, id;

-- name: DeleteWarehouseContacts :exec
DELETE FROM warehouse_contacts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND warehouse_id = sqlc.arg(warehouse_id)::bigint;

-- name: CreateWarehouseContact :exec
INSERT INTO warehouse_contacts (
    tenant_id, warehouse_id, contact_type, employee_id, name, phone, email, is_primary, status
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(warehouse_id)::bigint,
    sqlc.arg(contact_type)::text, NULLIF(sqlc.arg(employee_id)::bigint, 0),
    sqlc.arg(name)::text, sqlc.arg(phone)::text, sqlc.arg(email)::text,
    sqlc.arg(is_primary)::bool, sqlc.arg(status)::text
);

-- name: CreateWarehouseHistory :exec
INSERT INTO warehouse_change_history (
    tenant_id, warehouse_id, action, before_data, after_data, reason,
    changed_by, changed_by_name
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(warehouse_id)::bigint,
    sqlc.arg(action)::text, sqlc.arg(before_data)::jsonb, sqlc.arg(after_data)::jsonb,
    sqlc.arg(reason)::text, NULLIF(sqlc.arg(changed_by)::bigint, 0),
    sqlc.arg(changed_by_name)::text
);

-- name: GetWarehouseSettings :one
SELECT usage_mode, allow_direct_delivery, allow_inventory,
       default_warehouse_id, updated_at
FROM warehouse_settings
WHERE tenant_id = sqlc.arg(tenant_id)::bigint;

-- name: UpsertWarehouseSettings :exec
INSERT INTO warehouse_settings (
    tenant_id, usage_mode, allow_direct_delivery, allow_inventory,
    default_warehouse_id, updated_by, updated_at
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(usage_mode)::text,
    sqlc.arg(allow_direct_delivery)::bool, sqlc.arg(allow_inventory)::bool,
    NULLIF(sqlc.arg(default_warehouse_id)::bigint, 0),
    NULLIF(sqlc.arg(updated_by)::bigint, 0), now()
)
ON CONFLICT (tenant_id) DO UPDATE SET
    usage_mode = excluded.usage_mode,
    allow_direct_delivery = excluded.allow_direct_delivery,
    allow_inventory = excluded.allow_inventory,
    default_warehouse_id = excluded.default_warehouse_id,
    updated_by = excluded.updated_by,
    updated_at = now();

-- name: CreateWarehouseSettingsHistory :exec
INSERT INTO warehouse_settings_history (
    tenant_id, usage_mode, allow_direct_delivery, allow_inventory,
    default_warehouse_id, changed_by
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(usage_mode)::text,
    sqlc.arg(allow_direct_delivery)::bool, sqlc.arg(allow_inventory)::bool,
    NULLIF(sqlc.arg(default_warehouse_id)::bigint, 0),
    NULLIF(sqlc.arg(changed_by)::bigint, 0)
);

-- name: StockCostForUpdate :one
-- The average this row currently carries, locked so a concurrent receipt
-- cannot move it between the read and the movement priced against it.
SELECT id, avg_cost::text AS avg_cost, cost_currency,
       on_hand_qty::text AS on_hand_qty, total_cost::text AS total_cost
FROM stocks
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
FOR UPDATE;

-- name: ListStocks :many
SELECT
    s.id, s.warehouse_id, w.code AS warehouse_code, w.name AS warehouse_name,
    s.product_id, s.sku_id, s.product_code, s.product_name, s.uom_id, s.uom_code,
    s.cost_currency,
    s.total_cost::text AS total_cost,
    s.avg_cost::text   AS avg_cost,
    s.on_hand_qty::text AS on_hand_qty,
    s.reserved_qty::text AS reserved_qty,
    s.locked_qty::text AS locked_qty,
    s.frozen_qty::text AS frozen_qty,
    s.available_qty::text AS available_qty,
    s.updated_at,
    count(*) OVER () AS total
FROM stocks s
JOIN warehouses w ON w.id = s.warehouse_id
WHERE s.tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(warehouse_id)::bigint = 0 OR s.warehouse_id = sqlc.arg(warehouse_id)::bigint)
  AND (sqlc.arg(keyword)::text = ''
       OR s.product_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR s.product_code ILIKE '%' || sqlc.arg(keyword)::text || '%')
  -- "Only what is actually there" is the common question; rows that fell to
  -- zero are history, not stock.
  AND (NOT sqlc.arg(in_stock_only)::bool OR s.on_hand_qty > 0)
ORDER BY s.product_code, w.code
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: UpsertStockOnInbound :one
-- Receiving goods. The row is created on first receipt of a SKU into a
-- warehouse, so nothing has to pre-register a stock record.
--
-- Cost rides along: quantity and value move together or the average is wrong
-- from the next movement onwards. cost_currency is only set on creation —
-- changing it later would reinterpret a pool of money already in the row.
INSERT INTO stocks (
    tenant_id, warehouse_id, product_id, sku_id, uom_id, uom_code,
    product_code, product_name, on_hand_qty, total_cost, cost_currency
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(warehouse_id)::bigint,
    sqlc.arg(product_id)::bigint,
    sqlc.arg(sku_id)::bigint,
    sqlc.arg(uom_id)::bigint,
    sqlc.arg(uom_code)::text,
    sqlc.arg(product_code)::text,
    sqlc.arg(product_name)::text,
    sqlc.arg(qty)::text::numeric,
    sqlc.arg(amount)::text::numeric,
    sqlc.arg(cost_currency)::text
)
ON CONFLICT (tenant_id, warehouse_id, product_id, sku_id) DO UPDATE SET
    on_hand_qty  = stocks.on_hand_qty + excluded.on_hand_qty,
    total_cost   = stocks.total_cost + excluded.total_cost,
    product_code = excluded.product_code,
    product_name = excluded.product_name,
    updated_at   = now()
RETURNING id, on_hand_qty::text AS on_hand_qty, available_qty::text AS available_qty,
          avg_cost::text AS avg_cost, cost_currency;

-- name: StocksForItemForUpdate :many
-- Candidate rows to allocate from, locked in a stable order so two concurrent
-- reservations for the same item queue instead of interleaving.
--
-- Matched on product AND sku: sku_id 0 means the product has no variants, and
-- keying on sku alone would make every such product look like the same thing.
SELECT id, warehouse_id, available_qty::text AS available_qty
FROM stocks
WHERE tenant_id = $1
  AND product_id = sqlc.arg(product_id)
  AND sku_id = sqlc.arg(sku_id)
  AND available_qty > 0
ORDER BY id
FOR UPDATE;

-- name: AddReservedQty :one
UPDATE stocks SET
    reserved_qty = reserved_qty + sqlc.arg(qty)::text::numeric,
    updated_at   = now()
WHERE tenant_id = $1 AND id = sqlc.arg(id)
RETURNING on_hand_qty::text AS on_hand_qty, available_qty::text AS available_qty,
          avg_cost::text AS avg_cost;

-- name: ReleaseReservedQty :one
UPDATE stocks SET
    reserved_qty = reserved_qty - sqlc.arg(qty)::text::numeric,
    updated_at   = now()
WHERE tenant_id = $1 AND id = sqlc.arg(id)
RETURNING on_hand_qty::text AS on_hand_qty, available_qty::text AS available_qty,
          avg_cost::text AS avg_cost;

-- name: CreateReservation :one
-- The contract snapshot rides along because a later top-up has to re-announce
-- the shortage without calling back into export.
INSERT INTO stock_reservations (
    tenant_id, ref_type, ref_id, ref_line_id, ref_no,
    product_id, sku_id, demand_qty, reserved_qty,
    version_id, version_no, customer_name, delivery_date,
    product_code, product_name, uom_id, uom_code
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(ref_type)::text,
    sqlc.arg(ref_id)::bigint,
    sqlc.arg(ref_line_id)::bigint,
    sqlc.arg(ref_no)::text,
    sqlc.arg(product_id)::bigint,
    sqlc.arg(sku_id)::bigint,
    sqlc.arg(demand_qty)::text::numeric,
    0,
    sqlc.arg(version_id)::bigint,
    sqlc.arg(version_no)::int,
    sqlc.arg(customer_name)::text,
    sqlc.arg(delivery_date)::text,
    sqlc.arg(product_code)::text,
    sqlc.arg(product_name)::text,
    sqlc.arg(uom_id)::bigint,
    sqlc.arg(uom_code)::text
)
ON CONFLICT (tenant_id, ref_type, ref_id, ref_line_id) DO NOTHING
RETURNING id;

-- name: GetReservationByRef :one
SELECT id, ref_no, demand_qty::text AS demand_qty, reserved_qty::text AS reserved_qty,
       shortage_qty::text AS shortage_qty, status
FROM stock_reservations
WHERE tenant_id = $1 AND ref_type = sqlc.arg(ref_type)
  AND ref_id = sqlc.arg(ref_id) AND ref_line_id = sqlc.arg(ref_line_id);

-- name: AddReservationQty :one
UPDATE stock_reservations SET
    reserved_qty = reserved_qty + sqlc.arg(qty)::text::numeric,
    updated_at   = now()
WHERE tenant_id = $1 AND id = sqlc.arg(id)
RETURNING reserved_qty::text AS reserved_qty, shortage_qty::text AS shortage_qty;

-- name: AddReservationLine :exec
INSERT INTO stock_reservation_lines (tenant_id, reservation_id, stock_id, warehouse_id, qty)
VALUES ($1, sqlc.arg(reservation_id), sqlc.arg(stock_id), sqlc.arg(warehouse_id),
        sqlc.arg(qty)::text::numeric);

-- name: ListActiveReservationsFor :many
-- Everything a contract still holds, used when it is cancelled or replaced.
SELECT r.id, r.ref_line_id, r.sku_id, r.reserved_qty::text AS reserved_qty
FROM stock_reservations r
WHERE r.tenant_id = $1 AND r.ref_type = sqlc.arg(ref_type)
  AND r.ref_id = sqlc.arg(ref_id) AND r.status = 'ACTIVE';

-- name: ReservationLinesOf :many
SELECT id, stock_id, warehouse_id, qty::text AS qty
FROM stock_reservation_lines
WHERE tenant_id = $1 AND reservation_id = sqlc.arg(reservation_id);

-- name: SetReservationStatus :exec
UPDATE stock_reservations SET status = sqlc.arg(new_status)::text, updated_at = now()
WHERE tenant_id = $1 AND id = sqlc.arg(id);

-- name: AppendLedger :exec
INSERT INTO stock_ledger (
    tenant_id, stock_id, warehouse_id, sku_id, movement, qty,
    ref_type, ref_id, ref_no, on_hand_after, available_after, operator_id, remark,
    unit_cost, amount, avg_cost_after
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(stock_id)::bigint,
    sqlc.arg(warehouse_id)::bigint,
    sqlc.arg(sku_id)::bigint,
    sqlc.arg(movement)::text,
    sqlc.arg(qty)::text::numeric,
    sqlc.arg(ref_type)::text,
    sqlc.arg(ref_id)::bigint,
    sqlc.arg(ref_no)::text,
    sqlc.arg(on_hand_after)::text::numeric,
    sqlc.arg(available_after)::text::numeric,
    sqlc.arg(operator_id)::bigint,
    sqlc.arg(remark)::text,
    sqlc.arg(unit_cost)::text::numeric,
    sqlc.arg(amount)::text::numeric,
    sqlc.arg(avg_cost_after)::text::numeric
);

-- name: ListLedger :many
SELECT
    l.id, l.stock_id, l.warehouse_id, l.sku_id, l.movement,
    l.qty::text AS qty, l.ref_type, l.ref_id, l.ref_no,
    l.on_hand_after::text AS on_hand_after,
    l.available_after::text AS available_after,
    l.operator_id, l.remark, l.occurred_at,
    l.unit_cost::text      AS unit_cost,
    l.amount::text         AS amount,
    l.avg_cost_after::text AS avg_cost_after,
    s.cost_currency,
    s.product_code, s.product_name,
    count(*) OVER () AS total
FROM stock_ledger l
JOIN stocks s ON s.id = l.stock_id
WHERE l.tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(sku_id)::bigint = 0 OR l.sku_id = sqlc.arg(sku_id)::bigint)
  AND (sqlc.arg(movement)::text = '' OR l.movement = sqlc.arg(movement)::text)
ORDER BY l.occurred_at DESC, l.id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: FindStock :one
-- The row a receipt is about to land on, if it exists. Used to read the
-- current average before the receipt changes it, and to catch a currency that
-- would be silently averaged into a pool holding another.
SELECT id, cost_currency, avg_cost::text AS avg_cost, on_hand_qty::text AS on_hand_qty
FROM stocks
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND warehouse_id = sqlc.arg(warehouse_id)::bigint
  AND product_id = sqlc.arg(product_id)::bigint
  AND sku_id = sqlc.arg(sku_id)::bigint;

-- 下面两条只服务「第一次真的要用仓库时补一个默认仓库」，见 app/warehouseseed.go。

-- name: CountWarehouses :one
-- 任何状态都算：一条都没有 ≠ 有但都停用了。
SELECT count(*)::bigint FROM warehouses WHERE tenant_id = $1;

-- name: InsertWarehouseIfAbsent :execrows
INSERT INTO warehouses (tenant_id, code, name, wh_type) VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id, code) DO NOTHING;
