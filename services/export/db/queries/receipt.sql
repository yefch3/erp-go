-- name: AddReceiptAllocation :one
-- transaction_id 走 nullif(…, 0)：**0 表示「这笔核销不挂银行流水」**，
-- 不是「第 0 号流水」。新模型下的手工记账全部走这一条（传 0），老行还带着
-- 真实的流水号。用 0 而不是把参数改成可空类型，是为了不让 sqlc 生成的
-- 入参类型变成指针——那会波及每一个调用点，而这里只需要一个哨兵值。
INSERT INTO receipt_allocations (
    tenant_id, transaction_id, contract_id, contract_no, customer_name,
    amount, fee_amount, fee_category, currency, reversal_of, reverse_reason,
    allocated_by, allocated_by_name, received_at, note
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    nullif(sqlc.arg(transaction_id)::bigint, 0),
    sqlc.arg(contract_id)::bigint,
    sqlc.arg(contract_no)::text,
    sqlc.arg(customer_name)::text,
    sqlc.arg(amount)::text::numeric,
    sqlc.arg(fee_amount)::text::numeric,
    sqlc.arg(fee_category)::text,
    sqlc.arg(currency)::text,
    nullif(sqlc.arg(reversal_of)::bigint, 0),
    sqlc.arg(reverse_reason)::text,
    sqlc.arg(allocated_by)::bigint,
    sqlc.arg(allocated_by_name)::text,
    -- 空串表示不记到账日（老行就是这样：那个日子在银行流水那边）。
    nullif(sqlc.arg(received_at)::text, '')::date,
    sqlc.arg(note)::text
)
RETURNING id;

-- name: ListAllocationsOfTransaction :many
SELECT
    id, contract_id, contract_no, customer_name,
    amount::text AS amount, fee_amount::text AS fee_amount, fee_category, currency,
    coalesce(reversal_of, 0)::bigint AS reversal_of, reverse_reason,
    allocated_by_name, allocated_at
FROM receipt_allocations
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND transaction_id = sqlc.arg(transaction_id)::bigint
ORDER BY id;

-- name: AllocationSumsByTransactions :many
-- 一页流水的已核金额，一次问完。
--
-- 收款对账的列表原来对每一行单独查一次核销记录（20 行一页就是 20 次往返），
-- 而列表上只用得到一个和——完整的核销明细只有详情页要。
SELECT coalesce(transaction_id, 0)::bigint AS transaction_id, sum(amount)::text AS allocated
FROM receipt_allocations
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND transaction_id = ANY(sqlc.arg(transaction_ids)::bigint[])
GROUP BY transaction_id;

-- name: GetAllocation :one
SELECT
    id, coalesce(transaction_id, 0)::bigint AS transaction_id, contract_id, contract_no, customer_name,
    amount::text AS amount, fee_amount::text AS fee_amount, fee_category, currency,
    coalesce(reversal_of, 0)::bigint AS reversal_of
FROM receipt_allocations
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: AllocationReversed :one
-- Whether this row has already been undone. The partial unique index makes a
-- second reversal impossible anyway; this turns the constraint violation into
-- a readable refusal instead of a 500.
SELECT EXISTS (
    SELECT 1 FROM receipt_allocations
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
      AND reversal_of = sqlc.arg(allocation_id)::bigint
) AS reversed;

-- name: ContractReceiptProgress :one
-- What one contract is owed and has been paid. The basis is the in-force
-- version: a contract that was amended is owed what the amendment says, and
-- money received against the old figure still counts.
SELECT
    c.id, c.contract_no, c.customer_name,
    coalesce(v.currency, '')::text        AS currency,
    coalesce(v.total_amount, 0)::text     AS total_amount,
    coalesce(r.received, 0)::text         AS received_amount,
    (coalesce(v.total_amount, 0) - coalesce(r.received, 0))::text AS open_amount
FROM contracts c
LEFT JOIN contract_versions v ON v.id = c.current_version_id
LEFT JOIN (
    SELECT contract_id, sum(amount + fee_amount) AS received
    FROM receipt_allocations
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
    GROUP BY contract_id
) r ON r.contract_id = c.id
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint AND c.id = sqlc.arg(contract_id)::bigint;

-- name: ListAllocationsOfContract :many
-- The other half of the many-to-many: one contract collected in instalments.
--
-- 原来这里 JOIN bank_transactions 取流水号和到账日期。F2 之后银行流水只有一
-- 本、在采购库里，**跨库 JOIN 不了**，所以这里只出核销记录本身，流水那几列
-- 由上层拿 transaction_id 去账本取（见 app.ContractReceipts）。
--
-- 排序也跟着换成按核销时间：到账日期在另一个库里，SQL 排不了。两者顺序通常
-- 一致——钱到了才核销——不一致的时候，按核销时间排反而更贴合这个页面在回答
-- 的问题：这张合同的钱是**什么时候被认下来的**。
SELECT
    a.id, coalesce(a.transaction_id, 0)::bigint AS transaction_id,
    a.amount::text AS amount,
    a.fee_amount::text AS fee_amount, a.currency,
    coalesce(a.reversal_of, 0)::bigint AS reversal_of,
    a.reverse_reason,
    coalesce(a.received_at::text, '')::text AS received_at,
    a.note,
    a.allocated_by_name, a.allocated_at
FROM receipt_allocations a
WHERE a.tenant_id = sqlc.arg(tenant_id)::bigint
  AND a.contract_id = sqlc.arg(contract_id)::bigint
ORDER BY a.allocated_at DESC, a.id DESC;

-- name: OpenReceivables :many
-- Candidates for the allocation picker. Only the in-force version counts, and
-- only the same currency: cross-currency settlement creates an exchange gain
-- or loss, which belongs in a general ledger this system does not yet have.
SELECT
    c.id, c.contract_no, c.customer_id, c.customer_name,
    v.currency,
    v.total_amount::text              AS total_amount,
    coalesce(r.received, 0)::text     AS received_amount,
    (v.total_amount - coalesce(r.received, 0))::text AS open_amount
FROM contracts c
JOIN contract_versions v ON v.id = c.current_version_id
LEFT JOIN (
    SELECT contract_id, sum(amount + fee_amount) AS received
    FROM receipt_allocations
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
    GROUP BY contract_id
) r ON r.contract_id = c.id
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint
  AND c.status IN ('EFFECTIVE', 'EXECUTING', 'COMPLETED')
  AND (sqlc.arg(currency)::text = '' OR v.currency = sqlc.arg(currency)::text)
  AND (sqlc.arg(customer_id)::bigint = 0 OR c.customer_id = sqlc.arg(customer_id)::bigint)
  -- With no search term the picker shows only what is still owed; a search
  -- reaches settled contracts too, because sometimes the question is
  -- "did this one get paid".
  -- 两种候选：核销要「还欠钱的」，退款要「收过钱的」（退的上限就是已收）。
  AND (CASE WHEN sqlc.arg(for_refund)::bool
        THEN coalesce(r.received, 0) > 0
        ELSE (v.total_amount - coalesce(r.received, 0)) > 0
       END OR sqlc.arg(keyword)::text <> '')
  -- 结清的合同默认也不出现——它已经宣布「不用再核了」。钱真的又来了，
  -- 搜合同号还能找到它（和上面那条「搜索能到已收满的」同一个道理）。
  AND (sqlc.arg(keyword)::text <> '' OR NOT EXISTS (
      SELECT 1 FROM contract_receivable_closures cl
      WHERE cl.tenant_id = c.tenant_id AND cl.contract_id = c.id
        AND cl.revoked_at IS NULL
  ))
  AND (sqlc.arg(keyword)::text = ''
       OR c.contract_no   ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR c.customer_name ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY (v.total_amount - coalesce(r.received, 0)) DESC, c.id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: FindContractsByNo :many
-- Resolves contract numbers scraped out of a remittance line into real
-- contracts, so the queue can pre-fill a suggestion.
SELECT c.id, c.contract_no, c.customer_name, v.currency,
       (v.total_amount - coalesce(r.received, 0))::text AS open_amount
FROM contracts c
JOIN contract_versions v ON v.id = c.current_version_id
LEFT JOIN (
    SELECT contract_id, sum(amount + fee_amount) AS received
    FROM receipt_allocations
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
    GROUP BY contract_id
) r ON r.contract_id = c.id
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint
  AND c.contract_no = ANY(sqlc.arg(contract_nos)::text[]);

-- name: SetContractReceivableDue :exec
-- 改一份合同的应收到期日。
--
-- 上一版带 `AND receivable_due_date IS NULL`，因为那时唯一的写入者是「生效
-- 那一刻算一次」，只该算一次。现在写入者是人，改错了要能改回来，所以那道
-- 守卫撤掉——代价是催收那边要跟着处理，见 ClearContractReminders。
--
-- 空串表示清空，回到「没填」。旧值由调用方在同一个事务里先读，这里不用
-- RETURNING 兜圈子。
UPDATE contracts SET receivable_due_date = nullif(sqlc.arg(due_date)::text, '')::date
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: ContractReceivableDue :one
-- 单读一列，给留痕取旧值用。走 GetContract 太重，而且那一句还要联版本表。
SELECT coalesce(receivable_due_date::text, '')::text AS receivable_due_date
FROM contracts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

-- name: RecordContractDueChange :exec
INSERT INTO contract_due_changes
    (tenant_id, contract_id, old_due_date, new_due_date, reason, changed_by_id, changed_by_name)
VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(contract_id)::bigint,
    nullif(sqlc.arg(old_due_date)::text, '')::date,
    nullif(sqlc.arg(new_due_date)::text, '')::date,
    sqlc.arg(reason)::text,
    sqlc.arg(changed_by_id)::bigint,
    sqlc.arg(changed_by_name)::text
);

-- name: ClearContractReminders :execrows
-- 改完到期日，把这份合同还没读的催收提醒清掉。
--
-- 不清的话会重发一整轮：提醒的幂等键里带着 due_date（00014 的唯一键），
-- 日子一换，同一档提醒就成了「新的一条」，扫描器下一轮把 SOON / DUE /
-- OVERDUE 全部再发一遍。那个设计当初是对的——那时唯一能改到期日的是补算，
-- 日子变了确实该重新提醒；现在人手一改就重发，改错再改回就是两轮。
--
-- 只清未读：读过的是历史，销售看见过就是看见过，不该被抹掉。
UPDATE receivable_reminders SET read_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND contract_id = sqlc.arg(contract_id)::bigint
  AND read_at IS NULL;

-- name: ListReceivableDue :many
-- 财务的到期清单：还没收完的生效合同，按该收的日子排，逾期的在最前。
--
-- 「还没收完」是算出来的而不是存的状态——sum(核销) < 合同金额。核销表
-- append-only（冲销是负行），所以求和天然反映当下的真相。
--
-- overdue_days 正数表示已逾期，负数表示还有几天到期；到期日为空的合同
-- 排在最后，它们缺的是客户账期配置，不是钱。
SELECT
    c.id, c.contract_no, c.customer_id, c.customer_name,
    c.sales_employee_id, c.sales_employee,
    coalesce(c.receivable_due_date::text, '')::text AS due_date,
    coalesce(c.effective_at::date::text, '')::text  AS effective_date,
    coalesce(v.currency, '')::text                  AS currency,
    coalesce(v.total_amount, 0)::text               AS total_amount,
    coalesce(r.received, 0)::text                   AS received_amount,
    (coalesce(v.total_amount, 0) - coalesce(r.received, 0))::text AS open_amount,
    coalesce((current_date - c.receivable_due_date), 0)::int       AS overdue_days,
    (c.receivable_due_date IS NULL)::bool                          AS due_unset,
    coalesce(cl.category, '')::text        AS closed_category,
    coalesce(cl.note, '')::text            AS closed_note,
    coalesce(cl.closed_by_name, '')::text  AS closed_by_name,
    coalesce(cl.created_at::text, '')::text AS closed_at,
    count(*) OVER () AS total
FROM contracts c
JOIN contract_versions v ON v.id = c.current_version_id
LEFT JOIN (
    SELECT contract_id, sum(amount + fee_amount) AS received
    FROM receipt_allocations
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
    GROUP BY contract_id
) r ON r.contract_id = c.id
-- 活着的结清（一张合同至多一条，部分唯一索引保证）。
LEFT JOIN contract_receivable_closures cl
    ON cl.tenant_id = c.tenant_id AND cl.contract_id = c.id AND cl.revoked_at IS NULL
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint
  AND c.status IN ('EFFECTIVE', 'EXECUTING')
  -- 两页：待核销 = 没有活着的结清；已完成 = 有。
  --
  -- **这里故意不看「还欠多少」。** 需求明说「是否核销完需要员工手动确认，
  -- 不一定数字对不上就不能完成，也不一定数字一样就算完成」——所以收满的
  -- 合同在没人点确认之前照样留在待核销页上，等人来点。原来那句
  -- `未收 > 0.01` 正是「数字对上就自动完成」，是这次要拆掉的东西。
  --
  -- 注意**不要**顺手把催收扫描（SweepReceivableReminders）里那句同样的
  -- `未收 > 0.01` 一起删掉：那一句回答的是另一个问题——「客户还欠钱吗」。
  -- 钱已经收齐、只是没人点确认的合同，销售不该收到催款提醒。两处从此
  -- 口径不同，这是对的，别再把它们"改一致"。
  AND (CASE WHEN sqlc.arg(closed_only)::bool
        THEN cl.id IS NOT NULL
        ELSE cl.id IS NULL
       END)
  -- 数据范围：应收是钱的事，沿用出口模块自己的围栏（同合同列表）。
  AND (sqlc.arg(scope_all)::bool OR c.sales_employee_id = ANY(sqlc.arg(employee_ids)::bigint[]))
  -- 只看逾期 / 只看未配账期，两个互斥的筛子，都不给就是全部。
  AND (sqlc.arg(overdue_only)::bool = false
       OR (c.receivable_due_date IS NOT NULL AND c.receivable_due_date < current_date))
  AND (sqlc.arg(unset_only)::bool = false OR c.receivable_due_date IS NULL)
  AND (sqlc.arg(keyword)::text = ''
       OR c.contract_no ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR c.customer_name ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY c.receivable_due_date ASC NULLS LAST, c.id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: SweepReceivableReminders :execrows
-- 一趟扫出所有该提醒而未提醒的合同，直接写成站内信。
--
-- 幂等全靠唯一键：同一张合同、同一个人、同一档、同一轮、同一个到期日
-- 只会有一行。所以 worker 多跑几次、停几天再补跑，结果都一样。
--
-- 档位用**范围**判断而不是等号——「今天正好是到期前 30 天」这种写法，
-- worker 那天没跑就永远错过。范围加唯一键则是：第一次进入范围时发一条，
-- 之后再扫都撞唯一键，什么也不发。
--
-- 逾期按 7 天一轮：period_no = ceil(逾期天数 / 7)，所以逾期第 1–7 天是
-- 第一轮，8–14 天是第二轮。轮次进了唯一键，于是每周恰好再响一次。
INSERT INTO receivable_reminders (
    tenant_id, contract_id, contract_no, customer_name, recipient_employee_id,
    reminder_type, period_no, due_date, open_amount, currency, title, content, detail_url
)
SELECT
    c.tenant_id, c.id, c.contract_no, c.customer_name, c.sales_employee_id,
    d.reminder_type, d.period_no, c.receivable_due_date,
    (v.total_amount - coalesce(r.received, 0)), v.currency,
    -- 提醒点进去落在「客户对账」的待核销那一档。老行里存的是历史地址
    -- （/receivable-due、/receivable-cases），那几条都保留成带 query 的
    -- 重定向，所以旧提醒照样点得开。
    d.title, d.content, '/customer-recon'
FROM contracts c
JOIN contract_versions v ON v.id = c.current_version_id
LEFT JOIN (
    SELECT tenant_id, contract_id, sum(amount + fee_amount) AS received
    FROM receipt_allocations
    GROUP BY tenant_id, contract_id
) r ON r.tenant_id = c.tenant_id AND r.contract_id = c.id
CROSS JOIN LATERAL (
    SELECT
        CASE
            WHEN current_date > c.receivable_due_date THEN 'OVERDUE'
            WHEN c.receivable_due_date - current_date <= 7 THEN 'DUE'
            ELSE 'SOON'
        END AS reminder_type,
        CASE
            WHEN current_date > c.receivable_due_date
            THEN ceil((current_date - c.receivable_due_date)::numeric / 7)::int
            ELSE 0
        END AS period_no,
        CASE
            WHEN current_date > c.receivable_due_date
            THEN c.contract_no || ' 应收逾期 ' || (current_date - c.receivable_due_date) || ' 天'
            WHEN c.receivable_due_date = current_date THEN c.contract_no || ' 今天到期'
            ELSE c.contract_no || ' 还有 ' || (c.receivable_due_date - current_date) || ' 天到期'
        END AS title,
        c.customer_name || ' · 未收 ' || v.currency || ' ' ||
            to_char(v.total_amount - coalesce(r.received, 0), 'FM999999999990.00') ||
            ' · 应收日 ' || c.receivable_due_date AS content
) d
-- 跨租户扫描：worker 没有租户上下文，新租户也不该需要额外配置才被覆盖。
-- 每一行写回的仍是合同自己的 tenant_id。
WHERE c.status IN ('EFFECTIVE', 'EXECUTING')
  -- 结清的合同不再提醒。这是整个结清机制存在的第一理由：一笔退款让未收
  -- 重新变正之后，没有这一条，销售每 7 天收一封「应收逾期」，永不停止
  -- （period_no 一直涨，唯一键永远撞不上）。
  AND NOT EXISTS (
      SELECT 1 FROM contract_receivable_closures cl
      WHERE cl.tenant_id = c.tenant_id AND cl.contract_id = c.id
        AND cl.revoked_at IS NULL
  )
  AND c.receivable_due_date IS NOT NULL
  -- 没有负责人就没有收件人。这类合同在清单页上仍然看得见，只是没人被点名。
  AND c.sales_employee_id > 0
  -- 还真的欠着钱才催。**这一句和应收清单页那边的条件从此不同口径，是有意
  -- 的，别再"改一致"**：清单页回答「财务确认过没有」（看有没有活着的结清），
  -- 这里回答「客户还欠钱吗」。钱收齐了但还没人点确认的合同，留在待核销页
  -- 上等人处理是对的，给销售发一封「应收逾期」催客户要钱就是错的。
  AND (v.total_amount - coalesce(r.received, 0)) > 0.01
  -- 只在进入视野之后才提醒：30 天以外的不打扰。
  AND c.receivable_due_date - current_date <= 30
ON CONFLICT (tenant_id, contract_id, recipient_employee_id, reminder_type, period_no, due_date)
DO NOTHING;

-- name: ListReceivableReminders :many
-- 某人的应收提醒收件箱。未读在前，同一批里新的在前。
SELECT id, contract_id, contract_no, customer_name, reminder_type, period_no,
       due_date::text AS due_date, open_amount::text AS open_amount, currency,
       title, content, detail_url, created_at,
       (read_at IS NULL)::bool AS unread,
       count(*) FILTER (WHERE read_at IS NULL) OVER () AS unread_total
FROM receivable_reminders
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND recipient_employee_id = sqlc.arg(employee_id)::bigint
  AND (sqlc.arg(unread_only)::bool = false OR read_at IS NULL)
-- id 收口：提醒是 sweeper 一趟批量插的，created_at 必然打平。top-N 没有
-- 唯一列收口时「取哪 50 条」不确定——刷新一次，看到的提醒可能换一批，
-- 「我刚才看到那条催款提醒，现在找不到了」。
ORDER BY (read_at IS NULL) DESC, created_at DESC, id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: MarkReceivableRemindersRead :execrows
-- 标记已读。只动自己的——收件箱是按人隔离的，别人的提醒不该被谁点掉。
UPDATE receivable_reminders SET read_at = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND recipient_employee_id = sqlc.arg(employee_id)::bigint
  AND read_at IS NULL
  AND (sqlc.arg(ids)::bigint[] = '{}' OR id = ANY(sqlc.arg(ids)::bigint[]));

-- 认差结清。四条一组，全部只碰活着的那一条（revoked_at IS NULL）。

-- name: GetLiveReceiptSettlement :one
SELECT id, transaction_id, amount::text AS amount, category, note,
       settled_by_name, created_at
FROM receipt_line_settlements
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND transaction_id = sqlc.arg(transaction_id)::bigint
  AND revoked_at IS NULL;

-- name: ListLiveReceiptSettlements :many
-- 列表页整页一次取，不是一行一问——和 AllocationSumsByTransactions 并排的
-- 同一个理由。
SELECT id, transaction_id, amount::text AS amount, category, note,
       settled_by_name, created_at
FROM receipt_line_settlements
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND transaction_id = ANY(sqlc.arg(transaction_ids)::bigint[])
  AND revoked_at IS NULL;

-- name: InsertReceiptSettlement :one
-- 撞 receipt_line_settlements_live 就是「已经结清过了」——并发的第二次点击
-- 在这里被唯一索引拦住，不靠先查后插。
INSERT INTO receipt_line_settlements (
    tenant_id, transaction_id, amount, category, note,
    settled_by_id, settled_by_name
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(transaction_id)::bigint,
    sqlc.arg(amount)::text::numeric,
    sqlc.arg(category)::text,
    sqlc.arg(note)::text,
    sqlc.arg(settled_by_id)::bigint,
    sqlc.arg(settled_by_name)::text
)
RETURNING id;

-- name: RevokeReceiptSettlement :execrows
-- WHERE 里的 revoked_at IS NULL 就是并发控制：两个人同时撤，只有一个改到行。
UPDATE receipt_line_settlements
SET revoked_at = now(),
    revoked_by_id = sqlc.arg(revoked_by_id)::bigint,
    revoked_by_name = sqlc.arg(revoked_by_name)::text,
    revoke_reason = sqlc.arg(revoke_reason)::text
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND transaction_id = sqlc.arg(transaction_id)::bigint
  AND revoked_at IS NULL;

-- 收款结清。表注释见 00019。

-- name: GetLiveReceivableClosure :one
SELECT id, contract_id, open_amount::text AS open_amount, category, note,
       closed_by_name, created_at
FROM contract_receivable_closures
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND contract_id = sqlc.arg(contract_id)::bigint
  AND revoked_at IS NULL;

-- name: InsertReceivableClosure :one
-- 撞 contract_receivable_closures_live = 已经结清过了。
INSERT INTO contract_receivable_closures (
    tenant_id, contract_id, open_amount, category, note,
    closed_by_id, closed_by_name
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(contract_id)::bigint,
    sqlc.arg(open_amount)::text::numeric,
    sqlc.arg(category)::text,
    sqlc.arg(note)::text,
    sqlc.arg(closed_by_id)::bigint,
    sqlc.arg(closed_by_name)::text
)
RETURNING id;

-- name: RevokeReceivableClosure :execrows
UPDATE contract_receivable_closures
SET revoked_at = now(),
    revoked_by_id = sqlc.arg(revoked_by_id)::bigint,
    revoked_by_name = sqlc.arg(revoked_by_name)::text,
    revoke_reason = sqlc.arg(revoke_reason)::text
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND contract_id = sqlc.arg(contract_id)::bigint
  AND revoked_at IS NULL;
