-- Purchase orders. Same rule as the rest of the system: quantities and money
-- cross the boundary as text, never float64.

-- name: RequirementsForOrder :many
-- The lines a buyer picked, locked so two people cannot order the same
-- outstanding quantity at the same time. open_qty is what is left to order —
-- a requirement may be spread over several orders.
SELECT
    id, contract_id, contract_no, contract_item_id, customer_name, source,
    product_id, coalesce(sku_id, 0)::bigint AS sku_id,
    product_code, product_name, spec, uom_id, uom_code, status,
    required_qty::text                 AS required_qty,
    ordered_qty::text                  AS ordered_qty,
    (required_qty - ordered_qty)::text AS open_qty,
    coalesce(required_date::text, '')::text AS required_date
FROM purchase_requirements
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = ANY (sqlc.arg(ids)::bigint[])
ORDER BY id
FOR UPDATE;

-- name: DraftReservedQtyForRequirements :many
-- Imported drafts reserve their demand until they are cancelled or ordered.
-- This is deliberately read after RequirementsForOrder has locked the
-- requirement rows, making simultaneous import confirmations serialize.
SELECT i.requirement_id, coalesce(sum(i.qty), 0)::text AS reserved_qty
FROM purchase_order_items i
JOIN purchase_orders o ON o.id = i.po_id AND o.tenant_id = i.tenant_id
WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint
  AND i.requirement_id = ANY(sqlc.arg(ids)::bigint[])
  AND o.status IN ('DRAFT', 'REJECTED', 'PENDING_APPROVAL')
GROUP BY i.requirement_id;

-- name: AddRequirementOrdered :one
-- Ordering moves a requirement along. Fully covered means ORDERED; partly
-- covered means somebody still has to buy the rest.
UPDATE purchase_requirements SET
    ordered_qty = ordered_qty + sqlc.arg(qty)::text::numeric,
    status = CASE
                 WHEN ordered_qty + sqlc.arg(qty)::text::numeric >= required_qty
                 THEN 'ORDERED' ELSE 'PARTIALLY_ORDERED'
             END,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND status IN ('PENDING', 'PARTIALLY_ORDERED')
  AND ordered_qty + sqlc.arg(qty)::text::numeric <= required_qty
RETURNING ordered_qty::text AS ordered_qty, status;

-- name: ReleaseRequirementOrdered :exec
-- An order that was rejected or cancelled never happened. What it had claimed
-- goes back on the buying list.
UPDATE purchase_requirements SET
    ordered_qty = greatest(ordered_qty - sqlc.arg(qty)::text::numeric, 0),
    status = CASE
                 WHEN status IN ('SUPERSEDED', 'CANCELLED') THEN status
                 WHEN greatest(ordered_qty - sqlc.arg(qty)::text::numeric, 0) <= 0
                 THEN 'PENDING'
                 ELSE 'PARTIALLY_ORDERED'
             END,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: AddRequirementReceived :one
-- Goods turned up. This is the only path that closes a manually raised
-- requirement: it has no contract behind it, so no amount of stock arriving
-- for somebody else can ever satisfy it — only its own order being delivered.
UPDATE purchase_requirements SET
    received_qty = received_qty + sqlc.arg(qty)::text::numeric,
    status = CASE
                 WHEN received_qty + sqlc.arg(qty)::text::numeric >= required_qty
                 THEN 'RECEIVED' ELSE status
             END,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
RETURNING received_qty::text AS received_qty, status;

-- name: CreatePurchaseOrder :one
INSERT INTO purchase_orders (
    tenant_id, po_no, supplier_id, supplier_code, supplier_name,
    currency, total_amount, expected_date, buyer_id, buyer_name, remark
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(po_no)::text,
    sqlc.arg(supplier_id)::bigint,
    sqlc.arg(supplier_code)::text,
    sqlc.arg(supplier_name)::text,
    sqlc.arg(currency)::text,
    sqlc.arg(total_amount)::text::numeric,
    nullif(sqlc.arg(expected_date)::text, '')::date,
    sqlc.arg(buyer_id)::bigint,
    sqlc.arg(buyer_name)::text,
    sqlc.arg(remark)::text
)
RETURNING id, po_no, status, created_at;

-- name: UpdatePurchaseOrderDraft :one
UPDATE purchase_orders SET
    supplier_id = sqlc.arg(supplier_id)::bigint,
    supplier_code = sqlc.arg(supplier_code)::text,
    supplier_name = sqlc.arg(supplier_name)::text,
    currency = sqlc.arg(currency)::text,
    total_amount = sqlc.arg(total_amount)::text::numeric,
    expected_date = nullif(sqlc.arg(expected_date)::text, '')::date,
    buyer_id = sqlc.arg(buyer_id)::bigint,
    buyer_name = sqlc.arg(buyer_name)::text,
    remark = sqlc.arg(remark)::text,
    status = 'DRAFT',
    approval_instance_id = NULL,
    reject_reason = '',
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND status IN ('DRAFT', 'REJECTED')
RETURNING id, po_no, status, created_at;

-- name: DeletePurchaseOrderItems :exec
DELETE FROM purchase_order_items
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND po_id = sqlc.arg(po_id)::bigint;

-- name: CreatePurchaseOrderItem :one
INSERT INTO purchase_order_items (
    tenant_id, po_id, requirement_id, product_id, sku_id,
    product_code, product_name, spec, uom_id, uom_code, qty, unit_price, amount
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(po_id)::bigint,
    sqlc.arg(requirement_id)::bigint,
    sqlc.arg(product_id)::bigint,
    nullif(sqlc.arg(sku_id)::bigint, 0),
    sqlc.arg(product_code)::text,
    sqlc.arg(product_name)::text,
    sqlc.arg(spec)::text,
    sqlc.arg(uom_id)::bigint,
    sqlc.arg(uom_code)::text,
    sqlc.arg(qty)::text::numeric,
    sqlc.arg(unit_price)::text::numeric,
    sqlc.arg(amount)::text::numeric
)
RETURNING id;

-- name: GetPurchaseOrderForUpdate :one
SELECT id, po_no, supplier_id, supplier_code, supplier_name, currency,
       total_amount::text AS total_amount, status,
       coalesce(approval_instance_id, 0)::bigint AS approval_instance_id,
       buyer_id, buyer_name, remark,
       coalesce(expected_date::text, '')::text AS expected_date
FROM purchase_orders
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
FOR UPDATE;

-- name: GetPurchaseOrder :one
SELECT
    o.id, o.po_no, o.supplier_id, o.supplier_code, o.supplier_name, o.currency,
    o.total_amount::text AS total_amount, o.status,
    coalesce(o.approval_instance_id, 0)::bigint AS approval_instance_id,
    o.reject_reason, o.cancel_reason, o.buyer_id, o.buyer_name, o.remark,
    coalesce(o.expected_date::text, '')::text AS expected_date,
    o.send_status, o.sent_to, o.sent_at, o.sent_by_name, o.send_error,
    o.ordered_at, o.created_at
FROM purchase_orders o
WHERE o.tenant_id = sqlc.arg(tenant_id)::bigint AND o.id = sqlc.arg(id)::bigint;

-- name: PurchaseOrderByInstance :one
-- How a Kafka approval decision finds the order it belongs to.
SELECT id, po_no, status
FROM purchase_orders
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND approval_instance_id = sqlc.arg(instance_id)::bigint
FOR UPDATE;

-- name: SetPurchaseOrderSubmitted :exec
UPDATE purchase_orders SET
    status = 'PENDING_APPROVAL', approval_instance_id = sqlc.arg(instance_id)::bigint,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: SetPurchaseOrderStatus :exec
UPDATE purchase_orders SET status = sqlc.arg(new_status)::text, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: SetPurchaseOrderOrdered :exec
UPDATE purchase_orders SET status = 'ORDERED', ordered_at = now(), updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: SetPurchaseOrderRejected :exec
UPDATE purchase_orders SET
    status = 'REJECTED', reject_reason = sqlc.arg(reason)::text, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: SetPurchaseOrderCancelled :exec
UPDATE purchase_orders SET
    status = 'CANCELLED', cancel_reason = sqlc.arg(reason)::text, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: PurchaseOrderItems :many
SELECT
    i.id, i.requirement_id, i.product_id, coalesce(i.sku_id, 0)::bigint AS sku_id,
    i.product_code, i.product_name, i.spec, i.uom_id, i.uom_code,
    i.qty::text          AS qty,
    i.unit_price::text   AS unit_price,
    i.amount::text       AS amount,
    i.received_qty::text AS received_qty,
    r.contract_no, r.customer_name, r.source
FROM purchase_order_items i
JOIN purchase_requirements r ON r.id = i.requirement_id
WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint AND i.po_id = sqlc.arg(po_id)::bigint
ORDER BY i.id;

-- name: PurchaseOrderItemsForUpdate :many
SELECT id, requirement_id, product_id, coalesce(sku_id, 0)::bigint AS sku_id,
       product_code, product_name, uom_id, uom_code,
       qty::text          AS qty,
       unit_price::text   AS unit_price,
       received_qty::text AS received_qty
FROM purchase_order_items
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND po_id = sqlc.arg(po_id)::bigint
ORDER BY id
FOR UPDATE;

-- name: AddPurchaseOrderItemReceived :exec
UPDATE purchase_order_items SET
    received_qty = received_qty + sqlc.arg(qty)::text::numeric
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: PurchaseOrderReceiptProgress :one
-- Whether every line of an order has arrived. Computed from the lines rather
-- than counted along the way, so a partial receipt entered twice cannot leave
-- the header claiming a completeness the lines do not support.
SELECT
    count(*) FILTER (WHERE received_qty < qty) AS outstanding_lines,
    coalesce(sum(received_qty), 0)::text       AS received_total
FROM purchase_order_items
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND po_id = sqlc.arg(po_id)::bigint;

-- name: CreatePurchaseReceipt :one
INSERT INTO purchase_receipts (
    tenant_id, po_id, receipt_no, warehouse_id, operator_id, operator_name, remark
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(po_id)::bigint,
    sqlc.arg(receipt_no)::text,
    sqlc.arg(warehouse_id)::bigint,
    sqlc.arg(operator_id)::bigint,
    sqlc.arg(operator_name)::text,
    sqlc.arg(remark)::text
)
RETURNING id, receipt_no, received_at;

-- name: CreatePurchaseReceiptItem :exec
INSERT INTO purchase_receipt_items (tenant_id, receipt_id, po_item_id, qty)
VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(receipt_id)::bigint,
    sqlc.arg(po_item_id)::bigint,
    sqlc.arg(qty)::text::numeric
);

-- name: ListPurchaseOrders :many
SELECT
    o.id, o.po_no, o.supplier_id, o.supplier_name, o.currency,
    o.total_amount::text AS total_amount, o.status, o.buyer_name, o.remark,
    o.reject_reason, o.cancel_reason,
    coalesce(o.expected_date::text, '')::text AS expected_date,
    o.send_status, o.sent_to, o.sent_at, o.sent_by_name, o.send_error,
    o.created_at,
    (SELECT count(*) FROM purchase_order_items i WHERE i.po_id = o.id) AS item_count,
    coalesce((SELECT sum(i.qty) FROM purchase_order_items i WHERE i.po_id = o.id), 0)::text AS total_qty,
    coalesce((SELECT sum(i.received_qty) FROM purchase_order_items i WHERE i.po_id = o.id), 0)::text AS received_qty,
    count(*) OVER () AS total
FROM purchase_orders o
WHERE o.tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(status)::text = '' OR o.status = sqlc.arg(status)::text)
  AND (sqlc.arg(keyword)::text = ''
       OR o.po_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR o.supplier_name ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY o.created_at DESC, o.id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: ListPurchaseReceipts :many
SELECT r.id, r.receipt_no, r.warehouse_id, r.operator_name, r.remark, r.received_at,
       coalesce(sum(ri.qty), 0)::text AS total_qty
FROM purchase_receipts r
LEFT JOIN purchase_receipt_items ri ON ri.receipt_id = r.id
WHERE r.tenant_id = sqlc.arg(tenant_id)::bigint AND r.po_id = sqlc.arg(po_id)::bigint
GROUP BY r.id
ORDER BY r.received_at DESC, r.id DESC;

-- name: OrdersForRequirement :many
-- Which orders are covering this requirement, and how much of each has
-- arrived. A buyer looking at an outstanding line needs to know whether it is
-- unbought or merely undelivered — the two look identical on the list and
-- call for completely different action.
SELECT
    o.id, o.po_no, o.supplier_name, o.status, o.currency,
    coalesce(o.expected_date::text, '')::text AS expected_date,
    i.qty::text          AS qty,
    i.unit_price::text   AS unit_price,
    i.received_qty::text AS received_qty,
    o.created_at
FROM purchase_order_items i
JOIN purchase_orders o ON o.id = i.po_id
WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint
  AND i.requirement_id = sqlc.arg(requirement_id)::bigint
ORDER BY o.created_at DESC, o.id DESC;

-- name: ReopenRequirement :one
-- Put a closed requirement back on the buying list.
--
-- Only ones closed automatically or by hand without an order behind them.
-- Once a buyer has ordered against it the row belongs to that order, and
-- reopening would invite a second purchase of the same goods.
UPDATE purchase_requirements SET
    status        = 'PENDING',
    closed_reason = '',
    updated_at    = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND status IN ('CANCELLED', 'SUPERSEDED')
  AND ordered_qty = 0
RETURNING status;
