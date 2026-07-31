-- Outbound. Same rule as the rest of this service: quantities cross the
-- boundary as text, never float64.

-- name: ReservationForUpdate :one
-- The row that decides whether anything may leave the building for this
-- contract line. Locked because two pickers must not both commit the same
-- reserved quantity.
SELECT
    id, ref_line_id, ref_no, product_id, sku_id, product_code, product_name,
    uom_id, uom_code, version_id, version_no, customer_name, delivery_date,
    demand_qty::text    AS demand_qty,
    reserved_qty::text  AS reserved_qty,
    locked_qty::text    AS locked_qty,
    shipped_qty::text   AS shipped_qty,
    shortage_qty::text  AS shortage_qty,
    status
FROM stock_reservations
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND ref_type = sqlc.arg(ref_type)::text
  AND ref_id = sqlc.arg(ref_id)::bigint
  AND ref_line_id = sqlc.arg(ref_line_id)::bigint
FOR UPDATE;

-- name: ReservationsOfContract :many
-- Everything a contract holds, in the shape the allocation event needs. Used
-- to re-publish StockAllocated after a top-up so procurement can shrink or
-- close the requirement it raised.
SELECT
    id, ref_line_id, ref_no, product_id, sku_id, product_code, product_name,
    uom_id, uom_code, version_id, version_no, customer_name, delivery_date,
    demand_qty::text   AS demand_qty,
    reserved_qty::text AS reserved_qty,
    shortage_qty::text AS shortage_qty
FROM stock_reservations
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND ref_type = sqlc.arg(ref_type)::text
  AND ref_id = sqlc.arg(ref_id)::bigint
  AND status <> 'RELEASED'
ORDER BY ref_line_id;

-- name: ReservationLinesForUpdate :many
-- Warehouse rows this reservation still has uncommitted quantity on.
SELECT
    id, stock_id, warehouse_id,
    (qty - committed_qty)::text AS remaining_qty
FROM stock_reservation_lines
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND reservation_id = sqlc.arg(reservation_id)::bigint
  AND qty > committed_qty
ORDER BY id
FOR UPDATE;

-- name: CommitReservationLine :exec
UPDATE stock_reservation_lines
SET committed_qty = committed_qty + sqlc.arg(qty)::text::numeric
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: UncommitReservationLine :exec
UPDATE stock_reservation_lines
SET committed_qty = committed_qty - sqlc.arg(qty)::text::numeric
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: LockReservationQty :exec
UPDATE stock_reservations
SET locked_qty = locked_qty + sqlc.arg(qty)::text::numeric, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: UnlockReservationQty :exec
UPDATE stock_reservations
SET locked_qty = locked_qty - sqlc.arg(qty)::text::numeric, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: ShipReservationQty :one
-- Locked becomes shipped. A line whose whole demand has left is closed, so it
-- stops appearing on the shippable list.
UPDATE stock_reservations SET
    locked_qty  = locked_qty - sqlc.arg(qty)::text::numeric,
    shipped_qty = shipped_qty + sqlc.arg(qty)::text::numeric,
    status      = CASE
                      WHEN shipped_qty + sqlc.arg(qty)::text::numeric >= demand_qty
                      THEN 'SHIPPED' ELSE status
                  END,
    updated_at  = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
RETURNING shipped_qty::text AS shipped_qty, status;

-- name: ConvertReserveToLock :one
-- Layer 1 to layer 2. available_qty is generated from both columns, so moving
-- quantity between them leaves availability untouched — which is the point:
-- the goods were already off the market, they are now also off the shelf.
UPDATE stocks SET
    reserved_qty = reserved_qty - sqlc.arg(qty)::text::numeric,
    locked_qty   = locked_qty + sqlc.arg(qty)::text::numeric,
    updated_at   = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
RETURNING on_hand_qty::text AS on_hand_qty, available_qty::text AS available_qty,
          avg_cost::text AS avg_cost;

-- name: ReleaseLockToReserve :one
UPDATE stocks SET
    locked_qty   = locked_qty - sqlc.arg(qty)::text::numeric,
    reserved_qty = reserved_qty + sqlc.arg(qty)::text::numeric,
    updated_at   = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
RETURNING on_hand_qty::text AS on_hand_qty, available_qty::text AS available_qty,
          avg_cost::text AS avg_cost;

-- name: ConsumeLockOnOutbound :one
-- The goods physically leave. on_hand and locked fall by the same amount, so
-- availability does not move: what just shipped was never available.
--
-- Value leaves with them, at the average the row carried a moment ago. The
-- CTE captures that average before the update rewrites it — reading it
-- afterwards would price the shipment at the average it produced, which is
-- circular and wrong by exactly the amount that matters.
WITH before AS (
    SELECT id, avg_cost, cost_currency
    FROM stocks
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
    FOR UPDATE
)
UPDATE stocks s SET
    on_hand_qty = s.on_hand_qty - sqlc.arg(qty)::text::numeric,
    locked_qty  = s.locked_qty - sqlc.arg(qty)::text::numeric,
    -- greatest() guards the last unit out: rounding across many movements can
    -- leave a few thousandths behind, and a negative cost pool would fail the
    -- CHECK and abort a shipment that is otherwise perfectly correct.
    total_cost  = greatest(s.total_cost - sqlc.arg(qty)::text::numeric * b.avg_cost, 0),
    updated_at  = now()
FROM before b
WHERE s.id = b.id
RETURNING s.on_hand_qty::text AS on_hand_qty,
          s.available_qty::text AS available_qty,
          s.avg_cost::text AS avg_cost_after,
          b.avg_cost::text AS unit_cost,
          (sqlc.arg(qty)::text::numeric * b.avg_cost)::text AS amount,
          b.cost_currency;

-- name: CreateOutbound :one
INSERT INTO outbounds (
    tenant_id, outbound_no, outbound_type, ref_type, ref_id, ref_no,
    customer_name, operator_id, operator_name, remark
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(outbound_no)::text,
    sqlc.arg(outbound_type)::text,
    sqlc.arg(ref_type)::text,
    sqlc.arg(ref_id)::bigint,
    sqlc.arg(ref_no)::text,
    sqlc.arg(customer_name)::text,
    sqlc.arg(operator_id)::bigint,
    sqlc.arg(operator_name)::text,
    sqlc.arg(remark)::text
)
RETURNING id, outbound_no, status, created_at;

-- name: CreateOutboundItem :one
INSERT INTO outbound_items (
    tenant_id, outbound_id, reservation_id, ref_line_id,
    product_id, sku_id, product_code, product_name, uom_code, qty
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(outbound_id)::bigint,
    sqlc.arg(reservation_id)::bigint,
    sqlc.arg(ref_line_id)::bigint,
    sqlc.arg(product_id)::bigint,
    sqlc.arg(sku_id)::bigint,
    sqlc.arg(product_code)::text,
    sqlc.arg(product_name)::text,
    sqlc.arg(uom_code)::text,
    sqlc.arg(qty)::text::numeric
)
RETURNING id;

-- name: CreateOutboundItemStock :exec
INSERT INTO outbound_item_stocks (
    tenant_id, outbound_item_id, reservation_line_id, stock_id, warehouse_id, qty
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(outbound_item_id)::bigint,
    sqlc.arg(reservation_line_id)::bigint,
    sqlc.arg(stock_id)::bigint,
    sqlc.arg(warehouse_id)::bigint,
    sqlc.arg(qty)::text::numeric
);

-- name: GetOutboundForUpdate :one
SELECT id, outbound_no, outbound_type, ref_type, ref_id, ref_no,
       customer_name, status, operator_id, operator_name, remark
FROM outbounds
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
FOR UPDATE;

-- name: SetOutboundConfirmed :exec
UPDATE outbounds SET status = 'CONFIRMED', confirmed_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: SetOutboundCancelled :exec
UPDATE outbounds SET status = 'CANCELLED', cancelled_reason = sqlc.arg(reason)::text
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: OutboundItemsOf :many
SELECT id, reservation_id, ref_line_id, product_id, sku_id,
       product_code, product_name, uom_code, qty::text AS qty
FROM outbound_items
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND outbound_id = sqlc.arg(outbound_id)::bigint
ORDER BY id;

-- name: OutboundItemStocksOf :many
SELECT id, reservation_line_id, stock_id, warehouse_id, qty::text AS qty
FROM outbound_item_stocks
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND outbound_item_id = sqlc.arg(outbound_item_id)::bigint
ORDER BY id;

-- name: ListOutbounds :many
SELECT
    o.id, o.outbound_no, o.outbound_type, o.ref_type, o.ref_id, o.ref_no,
    o.customer_name, o.status, o.operator_id, o.operator_name, o.remark,
    o.cancelled_reason, o.confirmed_at, o.created_at,
    (SELECT count(*) FROM outbound_items i WHERE i.outbound_id = o.id) AS item_count,
    coalesce((SELECT sum(i.qty) FROM outbound_items i WHERE i.outbound_id = o.id), 0)::text AS total_qty,
    count(*) OVER () AS total
FROM outbounds o
WHERE o.tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(status)::text = '' OR o.status = sqlc.arg(status)::text)
  AND (sqlc.arg(keyword)::text = ''
       OR o.outbound_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR o.ref_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR o.customer_name ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY o.created_at DESC, o.id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: ListShippableContracts :many
-- Contracts with something still owed. A line is done when everything it was
-- promised has shipped, so a contract drops off this list on its own.
SELECT
    r.ref_id AS contract_id,
    max(r.ref_no)::text        AS contract_no,
    max(r.customer_name)::text AS customer_name,
    max(r.delivery_date)::text AS delivery_date,
    count(*)             AS line_count,
    sum(r.demand_qty)::text                              AS demand_qty,
    sum(r.reserved_qty - r.locked_qty - r.shipped_qty)::text AS ready_qty,
    sum(r.shortage_qty)::text                            AS shortage_qty,
    sum(r.locked_qty)::text                              AS locked_qty,
    sum(r.shipped_qty)::text                             AS shipped_qty,
    count(*) OVER () AS total
FROM stock_reservations r
WHERE r.tenant_id = sqlc.arg(tenant_id)::bigint
  AND r.ref_type = 'CONTRACT'
  AND r.status = 'ACTIVE'
  AND (sqlc.arg(keyword)::text = ''
       OR r.ref_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR r.customer_name ILIKE '%' || sqlc.arg(keyword)::text || '%')
GROUP BY r.ref_id
ORDER BY max(r.delivery_date), r.ref_id
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: ShippableLinesOf :many
-- One row per contract line, with the four numbers a picker needs: what was
-- sold, what stock is holding for it, what is already on a picking list, and
-- what is missing. ready_qty is the only quantity an outbound may draw on.
SELECT
    r.id, r.ref_line_id, r.product_id, r.sku_id, r.product_code, r.product_name,
    r.uom_code, r.status,
    r.demand_qty::text   AS demand_qty,
    r.reserved_qty::text AS reserved_qty,
    r.locked_qty::text   AS locked_qty,
    r.shipped_qty::text  AS shipped_qty,
    r.shortage_qty::text AS shortage_qty,
    (r.reserved_qty - r.locked_qty - r.shipped_qty)::text AS ready_qty,
    -- What could still be taken from the shelf right now if the shortage were
    -- topped up on the spot. Shown so the page can say "3 of the 8 missing
    -- have arrived" instead of a flat refusal.
    coalesce((
        SELECT sum(s.available_qty) FROM stocks s
        WHERE s.tenant_id = r.tenant_id
          AND s.product_id = r.product_id AND s.sku_id = r.sku_id
    ), 0)::text AS available_qty
FROM stock_reservations r
WHERE r.tenant_id = sqlc.arg(tenant_id)::bigint
  AND r.ref_type = 'CONTRACT'
  AND r.ref_id = sqlc.arg(ref_id)::bigint
  AND r.status <> 'RELEASED'
ORDER BY r.ref_line_id;

-- name: RefreshReservationSnapshot :exec
-- A redelivered contract event must not reserve twice, but it may well carry
-- a fresher description than the one already stored. Quantities are pointedly
-- absent: this only refreshes the labels.
UPDATE stock_reservations SET
    ref_no        = sqlc.arg(ref_no)::text,
    version_id    = sqlc.arg(version_id)::bigint,
    version_no    = sqlc.arg(version_no)::int,
    customer_name = sqlc.arg(customer_name)::text,
    delivery_date = sqlc.arg(delivery_date)::text,
    product_code  = sqlc.arg(product_code)::text,
    product_name  = sqlc.arg(product_name)::text,
    uom_id        = sqlc.arg(uom_id)::bigint,
    uom_code      = sqlc.arg(uom_code)::text
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: WaitingReservationsForItem :many
-- Contract lines still short of this item, soonest delivery first.
--
-- Order matters and is a business rule, not a detail: when a part shipment
-- arrives it goes to whoever is due first. Ordering by id instead would hand
-- it to whoever signed first, which is not the same thing and is wrong every
-- time an early contract has a late delivery date.
SELECT
    id, ref_id, ref_line_id, ref_no, product_id, sku_id,
    shortage_qty::text AS shortage_qty
FROM stock_reservations
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND ref_type = 'CONTRACT'
  AND status = 'ACTIVE'
  AND product_id = sqlc.arg(product_id)::bigint
  AND sku_id = sqlc.arg(sku_id)::bigint
  AND shortage_qty > 0
ORDER BY delivery_date, id
FOR UPDATE;

-- name: SupersededReservationsOf :many
-- Reservations left behind by an earlier version of a contract that has since
-- been changed. locked and shipped come along because they decide whether the
-- quantity can simply be handed back or whether a person has to look at it.
SELECT id, ref_line_id, ref_no, product_id, sku_id, version_no,
       reserved_qty::text AS reserved_qty,
       locked_qty::text   AS locked_qty,
       shipped_qty::text  AS shipped_qty
FROM stock_reservations
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND ref_type = sqlc.arg(ref_type)::text
  AND ref_id = sqlc.arg(ref_id)::bigint
  AND status = 'ACTIVE'
  AND version_id < sqlc.arg(keep_version_id)::bigint
ORDER BY id
FOR UPDATE;
