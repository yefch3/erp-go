-- Quantities cross this boundary as text, same rule as everywhere else in
-- this system: Go holds decimal strings and never a float64.

-- name: ListWarehouses :many
SELECT id, code, name, wh_type, address, manager_id, status, created_at
FROM warehouses
WHERE tenant_id = $1 AND (sqlc.arg(include_inactive)::bool OR status = 'ACTIVE')
ORDER BY code;

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
