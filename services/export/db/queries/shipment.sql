-- name: CreateShipment :one
INSERT INTO shipments (
    tenant_id, shipment_no, vessel_name, voyage_no, bl_no, container_no,
    port_of_discharge, etd, eta, remark, created_by, created_by_name
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(shipment_no)::text,
    sqlc.arg(vessel_name)::text,
    sqlc.arg(voyage_no)::text,
    sqlc.arg(bl_no)::text,
    sqlc.arg(container_no)::text,
    sqlc.arg(port_of_discharge)::text,
    nullif(sqlc.arg(etd)::text, '')::date,
    nullif(sqlc.arg(eta)::text, '')::date,
    sqlc.arg(remark)::text,
    sqlc.arg(created_by)::bigint,
    sqlc.arg(created_by_name)::text
)
RETURNING id;

-- name: UpdateShipmentHeader :execrows
-- Only a draft may be edited. Once the boat has sailed the paperwork is what
-- the forwarder and the customs broker hold; changing it here afterwards would
-- put our record out of step with theirs without anybody noticing.
UPDATE shipments SET
    vessel_name       = sqlc.arg(vessel_name)::text,
    voyage_no         = sqlc.arg(voyage_no)::text,
    bl_no             = sqlc.arg(bl_no)::text,
    container_no      = sqlc.arg(container_no)::text,
    port_of_discharge = sqlc.arg(port_of_discharge)::text,
    etd               = nullif(sqlc.arg(etd)::text, '')::date,
    eta               = nullif(sqlc.arg(eta)::text, '')::date,
    remark            = sqlc.arg(remark)::text,
    updated_at        = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND status = 'DRAFT';

-- name: LockShipment :one
SELECT id, shipment_no, status, vessel_name, voyage_no, bl_no, created_by
FROM shipments
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
FOR UPDATE;

-- name: GetShipment :one
SELECT
    id, shipment_no, vessel_name, voyage_no, bl_no, container_no,
    port_of_discharge,
    coalesce(etd::text, '')::text AS etd,
    coalesce(eta::text, '')::text AS eta,
    status, remark, created_by, created_by_name,
    created_at, shipped_at, arrived_at
FROM shipments
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: ListShipments :many
-- Visibility is decided by the cargo, not by the booking clerk. A consolidated
-- box carries several customers' goods, so "may this person see this shipment"
-- can only mean "is any contract on it one they may see" — there is no single
-- owner on the header to compare against.
SELECT
    s.id, s.shipment_no, s.vessel_name, s.voyage_no, s.bl_no, s.container_no,
    s.port_of_discharge,
    coalesce(s.etd::text, '')::text AS etd,
    coalesce(s.eta::text, '')::text AS eta,
    s.status, s.created_by_name, s.created_at, s.shipped_at, s.arrived_at,
    (SELECT count(*) FROM shipment_items si
      WHERE si.shipment_id = s.id)::int AS line_count,
    (SELECT count(DISTINCT si.contract_id) FROM shipment_items si
      WHERE si.shipment_id = s.id)::int AS contract_count,
    coalesce((SELECT string_agg(DISTINCT si.contract_no, ', ')
              FROM shipment_items si WHERE si.shipment_id = s.id), '')::text AS contract_nos,
    count(*) OVER () AS total
FROM shipments s
WHERE s.tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(visible_all)::bool
       OR EXISTS (
           SELECT 1 FROM shipment_items si
           JOIN contracts c ON c.id = si.contract_id
           WHERE si.shipment_id = s.id
             AND c.sales_employee_id = ANY(sqlc.arg(visible_ids)::bigint[])
       )
       -- A draft with no lines yet belongs to whoever is typing it, otherwise
       -- it would vanish from their list the moment they saved it.
       OR s.created_by = sqlc.arg(operator_id)::bigint)
  AND (sqlc.arg(status)::text = '' OR s.status = sqlc.arg(status)::text)
  AND (sqlc.arg(contract_id)::bigint = 0
       OR EXISTS (SELECT 1 FROM shipment_items si
                  WHERE si.shipment_id = s.id
                    AND si.contract_id = sqlc.arg(contract_id)::bigint))
  AND (sqlc.arg(keyword)::text = ''
       OR s.shipment_no  ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR s.vessel_name  ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR s.bl_no        ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR s.container_no ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY s.id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: AddShipmentItem :exec
INSERT INTO shipment_items (
    tenant_id, shipment_id, line_no, contract_id, contract_no, customer_name,
    contract_item_id, product_id, sku_id, product_code, product_name, spec,
    qty, uom_code
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(shipment_id)::bigint,
    sqlc.arg(line_no)::int,
    sqlc.arg(contract_id)::bigint,
    sqlc.arg(contract_no)::text,
    sqlc.arg(customer_name)::text,
    sqlc.arg(contract_item_id)::bigint,
    sqlc.arg(product_id)::bigint,
    sqlc.arg(sku_id)::bigint,
    sqlc.arg(product_code)::text,
    sqlc.arg(product_name)::text,
    sqlc.arg(spec)::text,
    sqlc.arg(qty)::text::numeric,
    sqlc.arg(uom_code)::text
);

-- name: DeleteShipmentItems :exec
DELETE FROM shipment_items
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND shipment_id = sqlc.arg(shipment_id)::bigint;

-- name: ItemsOfShipment :many
SELECT
    id, line_no, contract_id, contract_no, customer_name, contract_item_id,
    product_id, sku_id, product_code, product_name, spec,
    qty::text AS qty, uom_code
FROM shipment_items
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND shipment_id = sqlc.arg(shipment_id)::bigint
ORDER BY line_no;

-- name: SetShipmentStatus :execrows
UPDATE shipments SET
    status     = sqlc.arg(status)::text,
    shipped_at = CASE WHEN sqlc.arg(status)::text = 'SHIPPED' THEN now() ELSE shipped_at END,
    arrived_at = CASE WHEN sqlc.arg(status)::text = 'ARRIVED' THEN now() ELSE arrived_at END,
    updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND status = sqlc.arg(from_status)::text;

-- name: RemoveShipmentFromLedger :execrows
-- Cancelling a sailed shipment takes the goods back out of the progress
-- ledger. The shipment row itself stays, marked CANCELLED — the ledger is
-- derived data, the document is the record.
DELETE FROM contract_shipments
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND outbound_no = sqlc.arg(outbound_no)::text;

-- name: ContractsOnShipment :many
-- The distinct contracts a shipment touches, for the per-contract checks that
-- run at confirmation and for the notifications that follow it.
SELECT DISTINCT si.contract_id, si.contract_no,
       c.sales_employee_id, c.status AS contract_status,
       coalesce(c.current_version_id, 0)::bigint AS current_version_id
FROM shipment_items si
JOIN contracts c ON c.id = si.contract_id
WHERE si.tenant_id = sqlc.arg(tenant_id)::bigint
  AND si.shipment_id = sqlc.arg(shipment_id)::bigint;

-- name: VesselsForContract :many
-- What is on the water for one contract, so the salesperson can answer "which
-- boat is my customer's order on" without ringing the forwarder.
SELECT
    s.id, s.shipment_no, s.vessel_name, s.voyage_no, s.bl_no, s.container_no,
    s.port_of_discharge,
    coalesce(s.etd::text, '')::text AS etd,
    coalesce(s.eta::text, '')::text AS eta,
    s.status,
    sum(si.qty)::text AS qty
FROM shipments s
JOIN shipment_items si ON si.shipment_id = s.id
WHERE si.tenant_id = sqlc.arg(tenant_id)::bigint
  AND si.contract_id = sqlc.arg(contract_id)::bigint
  AND s.status <> 'CANCELLED'
GROUP BY s.id
ORDER BY s.etd DESC NULLS LAST, s.id DESC;
