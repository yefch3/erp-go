-- Money crosses this boundary as text, same rule as quotations: Go holds
-- decimal strings and never a float64. See pkg/money.
--
-- Statements that mix casts with parameters use named arguments throughout;
-- interleaving them with positional $n leaves sqlc unable to infer a type.

-- name: CreateContract :one
INSERT INTO contracts (
    tenant_id, contract_no, quotation_id, quote_no, customer_id, customer_name,
    sales_employee_id, sales_employee, receivable_due_date, created_by, updated_by
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(contract_no)::text,
    nullif(sqlc.arg(quotation_id)::bigint, 0),
    sqlc.arg(quote_no)::text,
    sqlc.arg(customer_id)::bigint,
    sqlc.arg(customer_name)::text,
    sqlc.arg(sales_employee_id)::bigint,
    sqlc.arg(sales_employee)::text,
    -- 应收到期日：这份合同的钱什么时候该收回来。**建合同的人填**，可空。
    -- 不再从客户主数据的账期推——同一个客户这一单谈 60 天、下一单要求
    -- 预付，都是常事。
    nullif(sqlc.arg(receivable_due_date)::text, '')::date,
    sqlc.arg(created_by)::bigint,
    sqlc.arg(created_by)::bigint
)
RETURNING id;

-- name: CreateExistingContract :one
INSERT INTO contracts (
    tenant_id, contract_no, external_contract_no, entry_source,
    customer_id, customer_name, status, sales_employee_id, sales_employee,
    receivable_due_date, opening_received_amount, file_pending,
    signed_at, effective_at, signature_source, created_by, updated_by
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(contract_no)::text,
    sqlc.arg(external_contract_no)::text,
    'EXISTING_CONTRACT',
    sqlc.arg(customer_id)::bigint,
    sqlc.arg(customer_name)::text,
    'EXECUTING',
    sqlc.arg(sales_employee_id)::bigint,
    sqlc.arg(sales_employee)::text,
    nullif(sqlc.arg(receivable_due_date)::text, '')::date,
    sqlc.arg(opening_received_amount)::text::numeric,
    sqlc.arg(file_pending)::bool,
    sqlc.arg(signed_date)::text::date,
    sqlc.arg(effective_date)::text::date,
    'MANUAL',
    sqlc.arg(created_by)::bigint,
    sqlc.arg(created_by)::bigint
)
RETURNING id;

-- name: GetContract :one
SELECT
    id, tenant_id, contract_no, coalesce(quotation_id, 0)::bigint AS quotation_id, quote_no,
    customer_id, customer_name, coalesce(current_version_id, 0)::bigint AS current_version_id,
    status, status_before_approval, sales_employee_id, sales_employee,
    coalesce(receivable_due_date::text, '')::text AS receivable_due_date,
    signature_source, signed_at, effective_at, completed_at, created_at,
    coalesce(condition_confirmed_at::text,'')::text AS condition_confirmed_at,
    condition_confirmation_note, coalesce(condition_confirmed_by,0)::bigint AS condition_confirmed_by,
    condition_confirmed_by_name, external_contract_no, entry_source,
    opening_received_amount::text AS opening_received_amount, file_pending
FROM contracts
WHERE tenant_id = $1 AND id = $2;

-- name: RecordContractConditionConfirmation :exec
UPDATE contracts SET condition_confirmed_at=sqlc.arg(confirmed_at)::text::timestamptz,
 condition_confirmation_note=sqlc.arg(note), condition_confirmed_by=sqlc.arg(confirmed_by),
 condition_confirmed_by_name=sqlc.arg(confirmed_by_name), updated_at=now(),updated_by=sqlc.arg(confirmed_by)
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id);

-- name: ListContracts :many
SELECT
    c.id, c.contract_no, c.quote_no, c.customer_id, c.customer_name, c.status,
    c.sales_employee_id, c.sales_employee, c.signed_at, c.effective_at, c.created_at, c.updated_at,
    c.external_contract_no, c.entry_source,
    coalesce(c.receivable_due_date::text, '')::text AS receivable_due_date,
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
  AND (sqlc.arg(status)::text = '' OR c.status = sqlc.arg(status)::text
 OR (sqlc.arg(status)::text='PENDING_APPROVAL' AND c.status='DRAFT')
 OR (sqlc.arg(status)::text='EXECUTING' AND c.status='EFFECTIVE'))
 AND (sqlc.arg(sales_employee_id)::bigint=0 OR c.sales_employee_id=sqlc.arg(sales_employee_id)::bigint)
  AND (sqlc.arg(customer_id)::bigint = 0 OR c.customer_id = sqlc.arg(customer_id)::bigint)
  AND (sqlc.arg(keyword)::text = ''
       OR c.contract_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR c.external_contract_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
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

-- name: CorrectExistingContractHeader :execrows
UPDATE contracts SET
    external_contract_no = sqlc.arg(external_contract_no)::text,
    signed_at = sqlc.arg(signed_date)::text::date,
    effective_at = sqlc.arg(effective_date)::text::date,
    receivable_due_date = nullif(sqlc.arg(receivable_due_date)::text, '')::date,
    updated_at = now(), updated_by = sqlc.arg(updated_by)::bigint
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND entry_source = 'EXISTING_CONTRACT'
  AND status <> 'CANCELLED';

-- name: CorrectExistingContractVersion :execrows
WITH correction_enabled AS (
    SELECT set_config('erp.contract_correction', 'on', true)
)
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
    terms = sqlc.arg(terms)::text
FROM correction_enabled
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND id = sqlc.arg(id)::bigint
  AND status = 'APPROVED';

-- name: AddExistingContractCorrection :exec
INSERT INTO contract_corrections (
    tenant_id, contract_id, contract_version_id, before_data, after_data,
    corrected_by, corrected_by_name
) VALUES (
    sqlc.arg(tenant_id)::bigint, sqlc.arg(contract_id)::bigint,
    sqlc.arg(contract_version_id)::bigint, sqlc.arg(before_data)::jsonb,
    sqlc.arg(after_data)::jsonb, sqlc.arg(corrected_by)::bigint,
    sqlc.arg(corrected_by_name)::text
);

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

-- name: AddExistingContractItem :one
INSERT INTO contract_items (
    tenant_id, contract_version_id, line_no, product_id, sku_id, product_code,
    product_name, spec, qty, uom_id, uom_code, unit_price, amount, hs_code, remark,
    opening_procured_qty, opening_arrived_qty, opening_shipped_qty
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
    sqlc.arg(remark)::text,
    sqlc.arg(opening_procured_qty)::text::numeric,
    sqlc.arg(opening_arrived_qty)::text::numeric,
    sqlc.arg(opening_shipped_qty)::text::numeric
)
RETURNING id;

-- name: FinalizeExistingContract :exec
UPDATE contracts SET current_version_id = sqlc.arg(version_id)::bigint,
    updated_at = now(), updated_by = sqlc.arg(updated_by)::bigint
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: ClearContractFilePending :exec
UPDATE contracts SET file_pending = FALSE, updated_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: ListContractItems :many
SELECT
    id, contract_version_id, line_no, product_id, sku_id, product_code, product_name,
    spec, qty::text AS qty, uom_id, uom_code,
    unit_price::text AS unit_price, amount::text AS amount, hs_code, remark,
    opening_procured_qty::text AS opening_procured_qty,
    opening_arrived_qty::text AS opening_arrived_qty,
    opening_shipped_qty::text AS opening_shipped_qty
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
WITH current_items AS (
    SELECT i.*,
           CASE WHEN i.product_id = 0 THEN i.product_name ELSE '' END AS fallback_product_name,
           CASE WHEN i.product_id = 0 THEN i.uom_code ELSE '' END AS fallback_uom_code
    FROM contract_items i
    WHERE i.tenant_id = sqlc.arg(tenant_id)::bigint
      AND i.contract_version_id = sqlc.arg(contract_version_id)::bigint
), shipped AS (
    SELECT cs.product_id, coalesce(cs.sku_id, 0) AS sku_id,
           CASE WHEN cs.product_id = 0 THEN ci.product_name ELSE '' END AS fallback_product_name,
           CASE WHEN cs.product_id = 0 THEN ci.uom_code ELSE '' END AS fallback_uom_code,
           sum(cs.qty) AS shipped
    FROM contract_shipments cs
    LEFT JOIN contract_items ci
      ON ci.tenant_id = cs.tenant_id AND ci.id = cs.contract_item_id
    WHERE cs.tenant_id = sqlc.arg(tenant_id)::bigint
      AND cs.contract_id = sqlc.arg(contract_id)::bigint
    GROUP BY cs.product_id, coalesce(cs.sku_id, 0),
             CASE WHEN cs.product_id = 0 THEN ci.product_name ELSE '' END,
             CASE WHEN cs.product_id = 0 THEN ci.uom_code ELSE '' END
)
SELECT
    min(i.line_no)::int         AS line_no,
    i.product_id,
    coalesce(i.sku_id, 0)::bigint AS sku_id,
    max(i.product_code)::text   AS product_code,
    max(i.product_name)::text   AS product_name,
    max(i.uom_code)::text       AS uom_code,
    sum(i.qty)::text            AS qty,
    (sum(i.opening_shipped_qty) + coalesce(max(sh.shipped), 0))::text AS shipped_qty,
    (sum(i.qty) - sum(i.opening_shipped_qty) - coalesce(max(sh.shipped), 0))::text AS remaining_qty
FROM current_items i
LEFT JOIN shipped sh
  ON sh.product_id = i.product_id
 AND sh.sku_id = coalesce(i.sku_id, 0)
 AND sh.fallback_product_name = i.fallback_product_name
 AND sh.fallback_uom_code = i.fallback_uom_code
GROUP BY i.product_id, coalesce(i.sku_id, 0), i.fallback_product_name, i.fallback_uom_code
ORDER BY min(i.line_no);

-- name: ShipmentsOfContract :many
SELECT outbound_no, contract_item_id, qty::text AS qty, shipped_at
FROM contract_shipments
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND contract_id = sqlc.arg(contract_id)::bigint
ORDER BY shipped_at DESC, id DESC;

-- name: ListContractExecution :many
-- 一行一合同的执行进程（D2）：钱谈成了多少、货走了多少、款收了多少。
--
-- 「这单到哪了」现在要翻四个页面：合同看金额、采购看订没订、出运看走没走、
-- 银行流水看收没收。四份答案分散在四个人手里，没有一个人看得见整件事。
--
-- 三条腿在同一个库里，所以一条 SQL 就够，不会有 N+1：合同、已出运、已收款。
-- 另外两条（采购下没下单、船到哪了）在别的服务，由网关按这一页的合同批量取
-- 回来拼上——一页两次调用，不是一行两次。
--
-- 出运和收款都折成金额，是为了让两个数直接可比：走了六成货、收了三成款，
-- 一眼看得出钱和货脱节。数量做不到这一点——一张合同上 100 吨和 50 件加不
-- 起来。
SELECT
    c.id, c.contract_no, c.customer_id, c.customer_name,
    c.sales_employee_id, c.sales_employee, c.status,
    coalesce(c.effective_at::date::text, '')::text  AS effective_date,
    coalesce(c.receivable_due_date::text, '')::text AS due_date,
    coalesce((current_date - c.receivable_due_date), 0)::int AS overdue_days,
    (c.receivable_due_date IS NULL)::bool            AS due_unset,
    coalesce(v.currency, '')::text                   AS currency,
    coalesce(v.total_amount, 0)::text                AS total_amount,
    (coalesce(o.opening_shipped_amount, 0) + coalesce(s.shipped_amount, 0))::text AS shipped_amount,
    (CASE WHEN c.opening_received_amount = 0 THEN coalesce(r.received, 0)
          ELSE c.opening_received_amount + coalesce(r.received, 0) END)::text AS received_amount,
    count(*) OVER () AS total
FROM contracts c
JOIN contract_versions v ON v.id = c.current_version_id
LEFT JOIN LATERAL (
    -- Imported contracts may already have shipped goods. Value the opening
    -- snapshot without fabricating historical outbound documents.
    SELECT round(sum(i.opening_shipped_qty * i.unit_price), 2) AS opening_shipped_amount
    FROM contract_items i
    WHERE i.tenant_id = c.tenant_id
      AND i.contract_version_id = c.current_version_id
) o ON true
LEFT JOIN LATERAL (
    -- 已出运折成金额，按产品配对——和 ShipmentProgressOf 同一个口径，理由
    -- 也一样：改版会重写明细行的 id，按行配对会让改版前发出去的货凭空消失。
    --
    -- 单价取生效版本的加权均价（同一产品occasionally 落在两行上），和收款用
    -- 生效版本总额是同一个立场：合同改了，按改后的算。
    --
    -- 已出运可能超过合同金额——改版把量调小、而货已经发了。不夹紧，露出来。
    -- 反过来，改版把某个产品整个删掉、而它已经发过货，那部分在这里算不出
    -- 金额（没有单价可依），点进合同详情按产品看得见。
    -- 折到分。除法算出来的均价带一长串小数，原样冒出去会在页面上显示成
    -- 25000.00000000000000000000；两位与合同金额、已收款同一个刻度。
    SELECT round(sum(sh.shipped * li.unit_price), 2) AS shipped_amount
    FROM (
        SELECT cs.product_id, coalesce(cs.sku_id, 0) AS sku_id,
               CASE WHEN cs.product_id = 0 THEN ci.product_name ELSE '' END AS fallback_product_name,
               CASE WHEN cs.product_id = 0 THEN ci.uom_code ELSE '' END AS fallback_uom_code,
               sum(cs.qty) AS shipped
        FROM contract_shipments cs
        LEFT JOIN contract_items ci
          ON ci.tenant_id = cs.tenant_id AND ci.id = cs.contract_item_id
        WHERE cs.tenant_id = c.tenant_id AND cs.contract_id = c.id
        GROUP BY cs.product_id, coalesce(cs.sku_id, 0),
                 CASE WHEN cs.product_id = 0 THEN ci.product_name ELSE '' END,
                 CASE WHEN cs.product_id = 0 THEN ci.uom_code ELSE '' END
    ) sh
    JOIN (
        SELECT product_id, coalesce(sku_id, 0) AS sku_id,
               CASE WHEN product_id = 0 THEN product_name ELSE '' END AS fallback_product_name,
               CASE WHEN product_id = 0 THEN uom_code ELSE '' END AS fallback_uom_code,
               CASE WHEN sum(qty) > 0 THEN sum(amount) / sum(qty) ELSE 0 END AS unit_price
        FROM contract_items
        WHERE tenant_id = c.tenant_id AND contract_version_id = c.current_version_id
        GROUP BY product_id, coalesce(sku_id, 0),
                 CASE WHEN product_id = 0 THEN product_name ELSE '' END,
                 CASE WHEN product_id = 0 THEN uom_code ELSE '' END
    ) li ON li.product_id = sh.product_id
        AND li.sku_id = sh.sku_id
        AND li.fallback_product_name = sh.fallback_product_name
        AND li.fallback_uom_code = sh.fallback_uom_code
) s ON true
LEFT JOIN (
    SELECT contract_id, sum(amount + fee_amount) AS received
    FROM receipt_allocations
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
    GROUP BY contract_id
) r ON r.contract_id = c.id
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint
  -- 数据范围沿用合同列表那道围栏：看得见这张合同，才看得见它的进度。
  AND (sqlc.arg(scope_all)::bool OR c.sales_employee_id = ANY(sqlc.arg(employee_ids)::bigint[]))
  -- 默认只看在跑的。签之前没什么进程可言，作废的也不必占地方。
  AND (sqlc.arg(status)::text <> '' OR c.status IN ('EFFECTIVE', 'EXECUTING', 'COMPLETED'))
  AND (sqlc.arg(status)::text = '' OR c.status = sqlc.arg(status)::text
 OR (sqlc.arg(status)::text='PENDING_APPROVAL' AND c.status='DRAFT')
 OR (sqlc.arg(status)::text='EXECUTING' AND c.status='EFFECTIVE'))
 AND (sqlc.arg(sales_employee_id)::bigint=0 OR c.sales_employee_id=sqlc.arg(sales_employee_id)::bigint)
  AND (sqlc.arg(customer_id)::bigint = 0 OR c.customer_id = sqlc.arg(customer_id)::bigint)
  AND (sqlc.arg(keyword)::text = ''
       OR c.contract_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR c.external_contract_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR c.customer_name ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY c.effective_at DESC NULLS LAST, c.id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;
