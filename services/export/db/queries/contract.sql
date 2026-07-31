-- Money crosses this boundary as text, same rule as quotations: Go holds
-- decimal strings and never a float64. See pkg/money.
--
-- Statements that mix casts with parameters use named arguments throughout;
-- interleaving them with positional $n leaves sqlc unable to infer a type.

-- name: CreateContract :one
INSERT INTO contracts (
    tenant_id, contract_no, quotation_id, quote_no, customer_id, customer_name,
    sales_employee_id, sales_employee, created_by, updated_by
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(contract_no)::text,
    nullif(sqlc.arg(quotation_id)::bigint, 0),
    sqlc.arg(quote_no)::text,
    sqlc.arg(customer_id)::bigint,
    sqlc.arg(customer_name)::text,
    sqlc.arg(sales_employee_id)::bigint,
    sqlc.arg(sales_employee)::text,
    sqlc.arg(created_by)::bigint,
    sqlc.arg(created_by)::bigint
)
RETURNING id;

-- name: GetContract :one
SELECT
    id, tenant_id, contract_no, coalesce(quotation_id, 0)::bigint AS quotation_id, quote_no,
    customer_id, customer_name, coalesce(current_version_id, 0)::bigint AS current_version_id,
    status, status_before_approval, sales_employee_id, sales_employee,
    signature_source, signed_at, effective_at, completed_at, created_at
FROM contracts
WHERE tenant_id = $1 AND id = $2;

-- name: ListContracts :many
SELECT
    c.id, c.contract_no, c.quote_no, c.customer_id, c.customer_name, c.status,
    c.sales_employee_id, c.sales_employee, c.signed_at, c.effective_at, c.created_at,
    coalesce(v.currency, '')::text AS currency,
    coalesce(v.total_amount, 0)::text AS total_amount,
    coalesce(v.base_amount, 0)::text AS base_amount,
    coalesce(v.version_no, 0)::int AS version_no,
    count(*) OVER () AS total
FROM contracts c
-- The newest version, not the in-force one: a list must still show a contract
-- that has never been signed.
LEFT JOIN LATERAL (
    SELECT version_no, currency, total_amount, base_amount
    FROM contract_versions
    WHERE contract_id = c.id
    ORDER BY version_no DESC
    LIMIT 1
) v ON true
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint
  -- Data scope. An empty id list with visible_all = false means "nothing",
  -- which is the right answer for someone with no scope at all; it must not
  -- silently widen to everything.
  AND (sqlc.arg(visible_all)::bool
       OR c.sales_employee_id = ANY(sqlc.arg(visible_ids)::bigint[])
       -- Documents this person was asked to approve are visible whatever the
       -- scope says; an approver who cannot read the contract cannot approve it.
       OR c.id = ANY(sqlc.arg(involved_ids)::bigint[]))
  AND (sqlc.arg(status)::text = '' OR c.status = sqlc.arg(status)::text)
  AND (sqlc.arg(customer_id)::bigint = 0 OR c.customer_id = sqlc.arg(customer_id)::bigint)
  AND (sqlc.arg(keyword)::text = ''
       OR c.contract_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR c.customer_name ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY c.id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: MarkContractPendingApproval :one
-- Remembers where the contract stood, so a rejection can put it back there
-- rather than guessing between "still a draft" and "already running".
UPDATE contracts SET
    status = 'PENDING_APPROVAL',
    status_before_approval = status,
    updated_at = now(), updated_by = sqlc.arg(updated_by)::bigint
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
RETURNING status_before_approval;

-- name: SetContractStatus :one
UPDATE contracts SET
    status = sqlc.arg(new_status)::text,
    status_before_approval = CASE WHEN sqlc.arg(new_status)::text = 'PENDING_APPROVAL'
                                  THEN status_before_approval ELSE '' END,
    signed_at = CASE WHEN sqlc.arg(new_status)::text = 'EFFECTIVE' AND signed_at IS NULL
                     THEN now() ELSE signed_at END,
    effective_at = CASE WHEN sqlc.arg(new_status)::text = 'EFFECTIVE' AND effective_at IS NULL
                        THEN now() ELSE effective_at END,
    completed_at = CASE WHEN sqlc.arg(new_status)::text = 'COMPLETED'
                        THEN now() ELSE completed_at END,
    updated_at = now(), updated_by = sqlc.arg(updated_by)::bigint
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
RETURNING status;

-- name: SetContractCurrentVersion :exec
UPDATE contracts SET
    current_version_id = sqlc.arg(version_id)::bigint,
    updated_at = now(), updated_by = sqlc.arg(updated_by)::bigint
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: LockContract :one
SELECT id, status, coalesce(current_version_id, 0)::bigint AS current_version_id
FROM contracts
WHERE tenant_id = $1 AND id = $2
FOR UPDATE;

-- name: CreateContractVersion :one
INSERT INTO contract_versions (
    tenant_id, contract_id, version_no, buyer_name, buyer_address,
    seller_name, seller_address, currency, incoterm,
    port_of_loading, port_of_discharge, payment_method,
    delivery_date, terms, total_amount, base_amount,
    fx_rate, fx_rate_at, fx_source, fx_base_currency, change_reason, created_by
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(contract_id)::bigint,
    sqlc.arg(version_no)::int,
    sqlc.arg(buyer_name)::text,
    sqlc.arg(buyer_address)::text,
    sqlc.arg(seller_name)::text,
    sqlc.arg(seller_address)::text,
    sqlc.arg(currency)::text,
    sqlc.arg(incoterm)::text,
    sqlc.arg(port_of_loading)::text,
    sqlc.arg(port_of_discharge)::text,
    sqlc.arg(payment_method)::text,
    nullif(sqlc.arg(delivery_date)::text, '')::date,
    sqlc.arg(terms)::text,
    sqlc.arg(total_amount)::text::numeric,
    sqlc.arg(base_amount)::text::numeric,
    sqlc.arg(fx_rate)::text::numeric,
    sqlc.arg(fx_rate_at)::timestamptz,
    sqlc.arg(fx_source)::text,
    sqlc.arg(fx_base_currency)::text,
    sqlc.arg(change_reason)::text,
    sqlc.arg(created_by)::bigint
)
RETURNING id;

-- name: UpdateContractVersion :execrows
UPDATE contract_versions SET
    buyer_name = sqlc.arg(buyer_name)::text,
    buyer_address = sqlc.arg(buyer_address)::text,
    seller_name = sqlc.arg(seller_name)::text,
    seller_address = sqlc.arg(seller_address)::text,
    incoterm = sqlc.arg(incoterm)::text,
    port_of_loading = sqlc.arg(port_of_loading)::text,
    port_of_discharge = sqlc.arg(port_of_discharge)::text,
    payment_method = sqlc.arg(payment_method)::text,
    delivery_date = nullif(sqlc.arg(delivery_date)::text, '')::date,
    terms = sqlc.arg(terms)::text,
    total_amount = sqlc.arg(total_amount)::text::numeric,
    base_amount = sqlc.arg(base_amount)::text::numeric
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint AND status = 'DRAFT';

-- name: GetContractVersion :one
SELECT
    id, contract_id, version_no, buyer_name, buyer_address, seller_name, seller_address,
    currency, incoterm, port_of_loading, port_of_discharge, payment_method,
    coalesce(delivery_date::text, '')::text AS delivery_date, terms,
    total_amount::text AS total_amount, base_amount::text AS base_amount,
    fx_rate::text AS fx_rate, fx_rate_at, fx_source, fx_base_currency,
    change_reason, status, created_at, created_by
FROM contract_versions
WHERE tenant_id = $1 AND id = $2;

-- name: LatestContractVersionID :one
-- Only the id, so callers go through GetContractVersion and there is one
-- row shape for a version rather than two that must be kept in step.
SELECT id FROM contract_versions
WHERE tenant_id = $1 AND contract_id = $2
ORDER BY version_no DESC
LIMIT 1;

-- name: ListContractVersions :many
SELECT
    id, contract_id, version_no, currency,
    total_amount::text AS total_amount, base_amount::text AS base_amount,
    coalesce(delivery_date::text, '')::text AS delivery_date,
    change_reason, status, created_at, created_by
FROM contract_versions
WHERE tenant_id = $1 AND contract_id = $2
ORDER BY version_no DESC;

-- name: SetContractVersionStatus :exec
UPDATE contract_versions SET status = sqlc.arg(new_status)::text
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: SupersedeOtherVersions :exec
UPDATE contract_versions SET status = 'SUPERSEDED'
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND contract_id = sqlc.arg(contract_id)::bigint
  AND id <> sqlc.arg(keep_id)::bigint
  AND status = 'APPROVED';

-- name: DeleteContractItems :exec
DELETE FROM contract_items WHERE tenant_id = $1 AND contract_version_id = $2;

-- name: AddContractItem :exec
INSERT INTO contract_items (
    tenant_id, contract_version_id, line_no, product_id, sku_id, product_code,
    product_name, spec, qty, uom_id, uom_code, unit_price, amount, hs_code, remark
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(contract_version_id)::bigint,
    sqlc.arg(line_no)::int,
    sqlc.arg(product_id)::bigint,
    sqlc.narg(sku_id)::bigint,
    sqlc.arg(product_code)::text,
    sqlc.arg(product_name)::text,
    sqlc.arg(spec)::text,
    sqlc.arg(qty)::text::numeric,
    sqlc.arg(uom_id)::bigint,
    sqlc.arg(uom_code)::text,
    sqlc.arg(unit_price)::text::numeric,
    sqlc.arg(amount)::text::numeric,
    sqlc.arg(hs_code)::text,
    sqlc.arg(remark)::text
);

-- name: ListContractItems :many
SELECT
    id, contract_version_id, line_no, product_id, sku_id, product_code, product_name,
    spec, qty::text AS qty, uom_id, uom_code,
    unit_price::text AS unit_price, amount::text AS amount, hs_code, remark
FROM contract_items
WHERE tenant_id = $1 AND contract_version_id = $2
ORDER BY line_no;

-- name: CreateContractAttachment :one
INSERT INTO contract_attachments (
    tenant_id, contract_id, contract_version_id, kind, file_name, file_key,
    content_type, size_bytes, uploaded_by, uploader_name, source
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(contract_id)::bigint,
    nullif(sqlc.arg(contract_version_id)::bigint, 0),
    sqlc.arg(kind)::text,
    sqlc.arg(file_name)::text,
    sqlc.arg(file_key)::text,
    sqlc.arg(content_type)::text,
    sqlc.arg(size_bytes)::bigint,
    sqlc.arg(uploaded_by)::bigint,
    sqlc.arg(uploader_name)::text,
    sqlc.arg(source)::text
)
RETURNING id;

-- name: ListContractAttachments :many
SELECT
    a.id, a.contract_id,
    coalesce(a.contract_version_id, 0)::bigint AS contract_version_id,
    coalesce(v.version_no, 0)::int AS version_no,
    a.kind, a.source, a.file_name, a.file_key, a.content_type, a.size_bytes,
    a.uploaded_at, a.uploaded_by, a.uploader_name
FROM contract_attachments a
LEFT JOIN contract_versions v ON v.id = a.contract_version_id
WHERE a.tenant_id = $1 AND a.contract_id = $2
ORDER BY a.uploaded_at DESC;

-- name: GetContractAttachment :one
SELECT
    a.id, a.contract_id,
    coalesce(a.contract_version_id, 0)::bigint AS contract_version_id,
    coalesce(v.version_no, 0)::int AS version_no,
    a.kind, a.source, a.file_name, a.file_key, a.content_type, a.size_bytes,
    a.uploaded_at, a.uploaded_by, a.uploader_name
FROM contract_attachments a
LEFT JOIN contract_versions v ON v.id = a.contract_version_id
WHERE a.tenant_id = $1 AND a.id = $2;

-- name: DeleteContractAttachment :execrows
DELETE FROM contract_attachments WHERE tenant_id = $1 AND id = $2;

-- name: SetContractOwner :execrows
UPDATE contracts SET
    sales_employee_id = sqlc.arg(owner_id),
    sales_employee    = sqlc.arg(owner_name),
    updated_by        = sqlc.arg(updated_by),
    updated_at        = now()
WHERE tenant_id = $1 AND id = sqlc.arg(id);

-- name: LiveContractForQuotation :one
-- The one contract a quotation may still have; cancelled ones are excluded by
-- the same rule as the unique index, because a cancelled contract does not
-- block a new one.
SELECT id, contract_no, status, sales_employee_id, sales_employee
FROM contracts
WHERE tenant_id = $1 AND quotation_id = sqlc.arg(quotation_id) AND status <> 'CANCELLED';

-- name: RecordOwnershipTransfer :one
-- Returns the same shape as ListOwnershipTransfers so a caller can report
-- exactly what it moved without reading the log back.
INSERT INTO ownership_transfers (
    tenant_id, biz_type, biz_id, biz_no,
    from_employee_id, from_employee, to_employee_id, to_employee,
    reason, transferred_by, transferred_by_name
) VALUES ($1, sqlc.arg(biz_type), sqlc.arg(biz_id), sqlc.arg(biz_no),
          sqlc.arg(from_employee_id), sqlc.arg(from_employee),
          sqlc.arg(to_employee_id), sqlc.arg(to_employee),
          sqlc.arg(reason), sqlc.arg(transferred_by), sqlc.arg(transferred_by_name))
RETURNING id, biz_type, biz_id, biz_no,
          from_employee_id, from_employee, to_employee_id, to_employee,
          reason, transferred_by, transferred_by_name, transferred_at;

-- name: ListOwnershipTransfers :many
SELECT
    id, biz_type, biz_id, biz_no,
    from_employee_id, from_employee, to_employee_id, to_employee,
    reason, transferred_by, transferred_by_name, transferred_at
FROM ownership_transfers
WHERE tenant_id = $1 AND biz_type = sqlc.arg(biz_type) AND biz_id = sqlc.arg(biz_id)
ORDER BY transferred_at DESC, id DESC;

-- name: CountSignedAttachments :one
-- Evidence that a specific version came back countersigned. Scoped to the
-- version on purpose: a scan of v1 says nothing about the v2 that replaced it.
SELECT count(*) FROM contract_attachments
WHERE tenant_id = $1 AND contract_version_id = sqlc.arg(contract_version_id)
  AND kind = 'SIGNED';

-- name: MarkContractSigned :exec
UPDATE contracts SET
    signature_source = sqlc.arg(signature_source)::text,
    updated_by       = sqlc.arg(updated_by),
    updated_at       = now()
WHERE tenant_id = $1 AND id = sqlc.arg(id);

-- name: RecordShipment :execrows
-- Goods left the warehouse against a contract line. Conflicts are dropped
-- silently: a redelivered outbound event is the normal case, not an error.
INSERT INTO contract_shipments (
    tenant_id, contract_id, contract_item_id, product_id, sku_id, outbound_no, qty
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(contract_id)::bigint,
    sqlc.arg(contract_item_id)::bigint,
    sqlc.arg(product_id)::bigint,
    sqlc.arg(sku_id)::bigint,
    sqlc.arg(outbound_no)::text,
    sqlc.arg(qty)::text::numeric
)
ON CONFLICT (tenant_id, outbound_no, contract_item_id) DO NOTHING;

-- name: ShipmentProgressOf :many
-- How much of each product on a contract has shipped.
--
-- Grouped by product rather than by contract line, and that is deliberate. A
-- contract change rewrites the lines with new ids, so shipments made against
-- the old version would show as zero on the new one — the goods physically
-- left and the page would say nothing had. The customer bought a product; a
-- version changes the terms, not what is in the crate.
--
-- remaining_qty can go negative, and that is the most useful thing this query
-- produces: it means the contract was reduced after goods had already
-- shipped. No automatic rule gets that right, so it is surfaced rather than
-- clamped to zero and quietly forgotten.
SELECT
    min(i.line_no)::int         AS line_no,
    i.product_id,
    coalesce(i.sku_id, 0)::bigint AS sku_id,
    max(i.product_code)::text   AS product_code,
    max(i.product_name)::text   AS product_name,
    max(i.uom_code)::text       AS uom_code,
    sum(i.qty)::text            AS qty,
    coalesce(max(sh.shipped), 0)::text AS shipped_qty,
    (sum(i.qty) - coalesce(max(sh.shipped), 0))::text AS remaining_qty
FROM contract_items i
LEFT JOIN (
    SELECT product_id, coalesce(sku_id, 0) AS sku_id, sum(qty) AS shipped
    FROM contract_shipments
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
      AND contract_id = sqlc.arg(contract_id)::bigint
    GROUP BY product_id, coalesce(sku_id, 0)
) sh ON sh.product_id = i.product_id AND sh.sku_id = coalesce(i.sku_id, 0)
WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint
  AND i.contract_version_id = sqlc.arg(contract_version_id)::bigint
GROUP BY i.product_id, coalesce(i.sku_id, 0)
ORDER BY min(i.line_no);

-- name: ShipmentsOfContract :many
SELECT outbound_no, contract_item_id, qty::text AS qty, shipped_at
FROM contract_shipments
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND contract_id = sqlc.arg(contract_id)::bigint
ORDER BY shipped_at DESC, id DESC;
