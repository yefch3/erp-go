-- Money and quantities cross this boundary as text, same rule as export: Go
-- holds decimal strings and never a float64. See pkg/money.
--
-- Statements that mix casts with parameters use named arguments throughout;
-- interleaving them with positional $n leaves sqlc unable to infer a type.

-- name: UpsertRequirement :one
-- Idempotent by contract line. A redelivered ContractEffective hits the
-- conflict and refreshes the snapshot rather than creating a second
-- requirement for the same thing.
--
-- Quantity is refreshed too, because export re-publishes the whole line set
-- for a version and the line is the authority on what was sold.
INSERT INTO purchase_requirements (
    tenant_id, contract_id, contract_no, contract_version_id, version_no,
    contract_item_id, customer_name, product_id, sku_id, product_code,
    product_name, spec, uom_id, uom_code, required_qty, required_date, source,
    owner_id, owner_name, quotation_id, quotation_no, sourcing_case_id,
    sourcing_line_id, supplier_quote_line_id, supplier_id, supplier_name,
    factory_id, factory_name, source_currency, source_unit_price, moq, lead_time,
    source_payment_terms, source_incoterm, source_valid_until
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(contract_id)::bigint,
    sqlc.arg(contract_no)::text,
    sqlc.arg(contract_version_id)::bigint,
    sqlc.arg(version_no)::int,
    sqlc.arg(contract_item_id)::bigint,
    sqlc.arg(customer_name)::text,
    sqlc.arg(product_id)::bigint,
    nullif(sqlc.arg(sku_id)::bigint, 0),
    sqlc.arg(product_code)::text,
    sqlc.arg(product_name)::text,
    sqlc.arg(spec)::text,
    sqlc.arg(uom_id)::bigint,
    sqlc.arg(uom_code)::text,
    sqlc.arg(required_qty)::text::numeric,
    nullif(sqlc.arg(required_date)::text, '')::date,
    sqlc.arg(source)::text,
    sqlc.arg(owner_id)::bigint,
    sqlc.arg(owner_name)::text,
    sqlc.arg(quotation_id)::bigint, sqlc.arg(quotation_no)::text,
    sqlc.arg(sourcing_case_id)::bigint, sqlc.arg(sourcing_line_id)::bigint,
    sqlc.arg(supplier_quote_line_id)::bigint, sqlc.arg(supplier_id)::bigint,
    sqlc.arg(supplier_name)::text, sqlc.arg(factory_id)::bigint, sqlc.arg(factory_name)::text,
    sqlc.arg(source_currency)::text, sqlc.arg(source_unit_price)::text::numeric,
    nullif(sqlc.arg(moq)::text,'')::numeric, sqlc.arg(lead_time)::text,
    sqlc.arg(source_payment_terms)::text, sqlc.arg(source_incoterm)::text,
    nullif(sqlc.arg(source_valid_until)::text,'')::date
)
ON CONFLICT (tenant_id, contract_item_id) DO UPDATE SET
    required_qty        = excluded.required_qty,
    required_date       = excluded.required_date,
    contract_no         = excluded.contract_no,
    contract_version_id = excluded.contract_version_id,
    version_no          = excluded.version_no,
    product_name        = excluded.product_name,
    source              = excluded.source,
    quotation_id        = excluded.quotation_id,
    quotation_no        = excluded.quotation_no,
    sourcing_case_id    = excluded.sourcing_case_id,
    sourcing_line_id    = excluded.sourcing_line_id,
    supplier_quote_line_id = excluded.supplier_quote_line_id,
    supplier_id         = excluded.supplier_id,
    supplier_name       = excluded.supplier_name,
    factory_id          = excluded.factory_id,
    factory_name        = excluded.factory_name,
    source_currency     = excluded.source_currency,
    source_unit_price   = excluded.source_unit_price,
    moq                 = excluded.moq,
    lead_time           = excluded.lead_time,
    source_payment_terms = excluded.source_payment_terms,
    source_incoterm     = excluded.source_incoterm,
    source_valid_until  = excluded.source_valid_until,
    -- 合同重发或换版时刷新属主：负责人转手后，新版本生效即改归属。
    -- 事件不带属主（0）则保留原值，别把已知的抹成未知。
    owner_id   = CASE WHEN excluded.owner_id <> 0 THEN excluded.owner_id
                      ELSE purchase_requirements.owner_id END,
    owner_name = CASE WHEN excluded.owner_id <> 0 THEN excluded.owner_name
                      ELSE purchase_requirements.owner_name END,
    -- A line that is short again is owed again. Without this a requirement
    -- retired by a contract change — or closed because stock briefly covered
    -- it — stays closed forever, and the shortage sits there with nobody
    -- buying anything. Only automatic closures are revived: once a buyer has
    -- ordered against it the row belongs to that order, and PENDING would
    -- invite a second purchase of the same goods.
    status = CASE
                 WHEN purchase_requirements.status IN ('SUPERSEDED', 'CANCELLED')
                      AND purchase_requirements.ordered_qty = 0
                 THEN 'PENDING' ELSE purchase_requirements.status
             END,
    closed_reason = CASE
                        WHEN purchase_requirements.status IN ('SUPERSEDED', 'CANCELLED')
                             AND purchase_requirements.ordered_qty = 0
                        THEN '' ELSE purchase_requirements.closed_reason
                    END,
    updated_at = now()
RETURNING id;

-- name: SupersedeRequirementsBefore :many
-- A contract change produces a new version with new line ids, so the previous
-- version's requirements no longer describe anything that is owed.
--
-- Only untouched ones are retired. A requirement somebody has already ordered
-- against is a real commitment to a supplier, and quietly cancelling it here
-- would hide a problem that a person needs to resolve.
UPDATE purchase_requirements SET
    status        = 'SUPERSEDED',
    closed_reason = sqlc.arg(reason)::text,
    updated_at    = now()
WHERE tenant_id = $1
  AND contract_id = sqlc.arg(contract_id)
  AND contract_version_id <> sqlc.arg(keep_version_id)
  AND status = 'PENDING'
RETURNING id, contract_item_id, product_name;

-- name: CountOrderedOnOldVersions :one
-- Requirements from a superseded version that were already ordered. Not an
-- error the consumer can fix, but a number a buyer has to be told about.
SELECT count(*) FROM purchase_requirements
WHERE tenant_id = $1
  AND contract_id = sqlc.arg(contract_id)
  AND contract_version_id <> sqlc.arg(keep_version_id)
  AND status IN ('PARTIALLY_ORDERED', 'ORDERED');

-- name: ListRequirements :many
SELECT
    id, contract_id, contract_no, contract_version_id, version_no,
    contract_item_id, customer_name, product_id,
    coalesce(sku_id, 0)::bigint AS sku_id,
    product_code, product_name, spec, uom_id, uom_code,
    required_qty::text AS required_qty,
    ordered_qty::text  AS ordered_qty,
    draft.reserved_qty::text AS reserved_qty,
    greatest(required_qty - ordered_qty - draft.reserved_qty, 0)::text AS available_qty,
    received_qty::text AS received_qty,
    coalesce(required_date::text, '')::text AS required_date,
    source, status, closed_reason, created_at,
    owner_id, owner_name,
    quotation_id, quotation_no, cost_scenario_id, cost_scenario_no,
    sourcing_case_id, sourcing_line_id, supplier_quote_line_id,
    supplier_id, supplier_code, supplier_name,
    factory_id, factory_code, factory_name,
    source_currency, source_unit_price::text AS source_unit_price,
    coalesce(moq::text,'')::text AS moq, lead_time,
    source_payment_terms,source_incoterm,coalesce(source_valid_until::text,'')::text AS source_valid_until,
    count(*) OVER () AS total
FROM purchase_requirements
LEFT JOIN LATERAL (
    SELECT coalesce(sum(i.qty), 0) AS reserved_qty
    FROM purchase_order_items i
    JOIN purchase_orders o ON o.id = i.po_id AND o.tenant_id = i.tenant_id
    WHERE i.tenant_id = purchase_requirements.tenant_id
      AND i.requirement_id = purchase_requirements.id
      AND o.status IN ('DRAFT', 'REJECTED', 'PENDING_APPROVAL')
) draft ON true
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  -- 数据范围（A1）：属主是合同负责人（手工需求是创建人）。owner_id=0 的
  -- 历史行只有 scope_all 能看见——fail-closed，错也只错在看不见。
  AND (sqlc.arg(scope_all)::bool OR owner_id = ANY(sqlc.arg(owner_ids)::bigint[]))
  AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
  AND (sqlc.arg(contract_id)::bigint = 0 OR contract_id = sqlc.arg(contract_id)::bigint)
  AND (sqlc.arg(keyword)::text = ''
       OR contract_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR product_name ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR product_code ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY
    -- Outstanding work first, then by when it is needed. A requirement with
    -- no date sorts last rather than first, which is what NULLS LAST buys.
    CASE WHEN status = 'PENDING' THEN 0 WHEN status = 'PARTIALLY_ORDERED' THEN 1 ELSE 2 END,
    required_date NULLS LAST,
    id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: GetRequirement :one
SELECT
    id, contract_id, contract_no, contract_version_id, version_no,
    contract_item_id, customer_name, product_id,
    coalesce(sku_id, 0)::bigint AS sku_id,
    product_code, product_name, spec, uom_id, uom_code,
    required_qty::text AS required_qty,
    ordered_qty::text  AS ordered_qty,
    received_qty::text AS received_qty,
    coalesce(required_date::text, '')::text AS required_date,
    source, status, closed_reason, created_at,
    owner_id, owner_name,
    quotation_id, quotation_no, cost_scenario_id, cost_scenario_no,
    sourcing_case_id, sourcing_line_id, supplier_quote_line_id,
    supplier_id, supplier_code, supplier_name,
    factory_id, factory_code, factory_name,
    source_currency, source_unit_price::text AS source_unit_price,
    coalesce(moq::text,'')::text AS moq, lead_time,
    source_payment_terms,source_incoterm,coalesce(source_valid_until::text,'')::text AS source_valid_until
FROM purchase_requirements
WHERE tenant_id = $1 AND id = $2;

-- name: CancelRequirement :one
UPDATE purchase_requirements SET
    status        = 'CANCELLED',
    closed_reason = sqlc.arg(reason)::text,
    updated_at    = now()
WHERE tenant_id = $1 AND id = sqlc.arg(id) AND status = 'PENDING'
RETURNING status;

-- name: CloseCoveredRequirement :execrows
-- Stock caught up with a line that used to be short. The requirement is not
-- deleted — a buyer who saw it yesterday deserves to find out what became of
-- it — and only untouched ones are closed: once something is on order, the
-- goods are coming whether stock covered it in the meantime or not.
UPDATE purchase_requirements SET
    status        = 'CANCELLED',
    closed_reason = sqlc.arg(reason)::text,
    updated_at    = now()
WHERE tenant_id = $1
  AND contract_item_id = sqlc.arg(contract_item_id)
  AND status = 'PENDING';

-- name: CreateManualRequirement :one
-- A buyer raising a requirement themselves: restocking, a long-lead item, a
-- deal with a supplier. Deliberately not tied to a contract — purchasing is
-- not only the tail end of a sale, and a company that can only buy what it
-- has already sold cannot keep stock at all.
INSERT INTO purchase_requirements (
    tenant_id, contract_id, contract_no, contract_version_id, version_no,
    -- No contract line to key on, so the id is the key. Negative to stay out
    -- of the way of real contract_item_ids in the same unique index.
    contract_item_id, customer_name, product_id, sku_id, product_code,
    product_name, spec, uom_id, uom_code, required_qty, required_date,
    source, closed_reason, owner_id, owner_name
) VALUES (
    sqlc.arg(tenant_id)::bigint, 0, '', 0, 0,
    -nextval('purchase_requirements_id_seq'),
    '', sqlc.arg(product_id)::bigint,
    nullif(sqlc.arg(sku_id)::bigint, 0),
    sqlc.arg(product_code)::text,
    sqlc.arg(product_name)::text,
    sqlc.arg(spec)::text,
    sqlc.arg(uom_id)::bigint,
    sqlc.arg(uom_code)::text,
    sqlc.arg(required_qty)::text::numeric,
    nullif(sqlc.arg(required_date)::text, '')::date,
    'MANUAL',
    sqlc.arg(remark)::text,
    sqlc.arg(owner_id)::bigint,
    sqlc.arg(owner_name)::text
)
RETURNING id;

-- name: UpsertQuotationRequirement :one
-- 客户接受报价后，已确认成本方案中的每条产品成为一条待下单明细。
-- 幂等键是“报价 + 询盘产品行”；Kafka 重投只刷新尚未下单的快照。
INSERT INTO purchase_requirements (
    tenant_id, contract_id, contract_no, contract_version_id, version_no,
    contract_item_id, customer_name, product_id, sku_id, product_code,
    product_name, spec, uom_id, uom_code, required_qty, source,
    quotation_id, quotation_no, cost_scenario_id, cost_scenario_no,
    sourcing_case_id, sourcing_line_id, supplier_quote_line_id,
    supplier_id, supplier_code, supplier_name,
    factory_id, factory_code, factory_name,
    source_currency, source_unit_price, moq, lead_time
) VALUES (
    sqlc.arg(tenant_id), 0, '', 0, 0,
    -nextval('purchase_requirements_id_seq'), sqlc.arg(customer_name),
    sqlc.arg(product_id), nullif(sqlc.arg(sku_id)::bigint,0), '',
    sqlc.arg(product_name), sqlc.arg(spec), 0, sqlc.arg(uom_code),
    sqlc.arg(required_qty)::text::numeric, 'CUSTOMER_QUOTATION',
    sqlc.arg(quotation_id), sqlc.arg(quotation_no),
    sqlc.arg(cost_scenario_id), sqlc.arg(cost_scenario_no),
    sqlc.arg(sourcing_case_id), sqlc.arg(sourcing_line_id), sqlc.arg(supplier_quote_line_id),
    sqlc.arg(supplier_id), sqlc.arg(supplier_code), sqlc.arg(supplier_name),
    sqlc.arg(factory_id), sqlc.arg(factory_code), sqlc.arg(factory_name),
    sqlc.arg(source_currency), sqlc.arg(source_unit_price)::text::numeric,
    nullif(sqlc.arg(moq)::text,'')::numeric, sqlc.arg(lead_time)
)
ON CONFLICT (tenant_id, quotation_id, sourcing_line_id)
    WHERE source='CUSTOMER_QUOTATION'
DO UPDATE SET
    customer_name=excluded.customer_name,
    product_name=excluded.product_name,
    spec=excluded.spec,
    required_qty=excluded.required_qty,
    supplier_id=excluded.supplier_id,
    supplier_code=excluded.supplier_code,
    supplier_name=excluded.supplier_name,
    factory_id=excluded.factory_id,
    factory_code=excluded.factory_code,
    factory_name=excluded.factory_name,
    source_currency=excluded.source_currency,
    source_unit_price=excluded.source_unit_price,
    moq=excluded.moq,
    lead_time=excluded.lead_time,
    updated_at=now()
WHERE purchase_requirements.ordered_qty=0
RETURNING id;

-- name: ContractProcurementProgress :many
-- 一页合同的采购进度（D2），一次问完。
--
-- 按**项数**而不是数量：一张合同上 100 吨钢卷和 50 件配件加不起来，折成
-- 金额又会把采购成本混进一张讲营收的表。「共 3 项，3 项订齐，2 项到齐」
-- 单位无关，也正是采购员口头汇报的说法。
--
-- 作废和被改版顶掉的行不算在内——它们不是没办完的活，是不存在的活。
--
-- 没有采购需求的合同不会出现在结果里；网关按合同补零，这样「一项都没有」
-- 和「查不到」在页面上是同一个答案：还没开始采购。
SELECT
    contract_id,
    count(*)::int                                                   AS total_lines,
    count(*) FILTER (WHERE ordered_qty  >= required_qty)::int        AS ordered_lines,
    count(*) FILTER (WHERE received_qty >= required_qty)::int        AS received_lines
FROM purchase_requirements
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND contract_id = ANY(sqlc.arg(contract_ids)::bigint[])
  AND status NOT IN ('CANCELLED', 'SUPERSEDED')
GROUP BY contract_id;
