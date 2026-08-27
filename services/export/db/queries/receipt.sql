-- name: AddReceiptAllocation :one
INSERT INTO receipt_allocations (
    tenant_id, transaction_id, contract_id, contract_no, customer_name,
    amount, fee_amount, currency, reversal_of, reverse_reason,
    allocated_by, allocated_by_name
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(transaction_id)::bigint,
    sqlc.arg(contract_id)::bigint,
    sqlc.arg(contract_no)::text,
    sqlc.arg(customer_name)::text,
    sqlc.arg(amount)::text::numeric,
    sqlc.arg(fee_amount)::text::numeric,
    sqlc.arg(currency)::text,
    nullif(sqlc.arg(reversal_of)::bigint, 0),
    sqlc.arg(reverse_reason)::text,
    sqlc.arg(allocated_by)::bigint,
    sqlc.arg(allocated_by_name)::text
)
RETURNING id;

-- name: ListAllocationsOfTransaction :many
SELECT
    id, contract_id, contract_no, customer_name,
    amount::text AS amount, fee_amount::text AS fee_amount, currency,
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
SELECT transaction_id, sum(amount)::text AS allocated
FROM receipt_allocations
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND transaction_id = ANY(sqlc.arg(transaction_ids)::bigint[])
GROUP BY transaction_id;

-- name: GetAllocation :one
SELECT
    id, transaction_id, contract_id, contract_no, customer_name,
    amount::text AS amount, fee_amount::text AS fee_amount, currency,
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
    a.id, a.transaction_id, a.amount::text AS amount,
    a.fee_amount::text AS fee_amount, a.currency,
    coalesce(a.reversal_of, 0)::bigint AS reversal_of,
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
  AND ((v.total_amount - coalesce(r.received, 0)) > 0 OR sqlc.arg(keyword)::text <> '')
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
-- 合同生效那一刻把到期日钉下来（E1）。只在为空时写，和 effective_at
-- 同一个哲学：第一次生效定的日子就是约定，之后客户改账期不再回头改它。
UPDATE contracts SET receivable_due_date = sqlc.arg(due_date)::text::date
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
  AND receivable_due_date IS NULL;

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
    count(*) OVER () AS total
FROM contracts c
JOIN contract_versions v ON v.id = c.current_version_id
LEFT JOIN (
    SELECT contract_id, sum(amount + fee_amount) AS received
    FROM receipt_allocations
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
    GROUP BY contract_id
) r ON r.contract_id = c.id
WHERE c.tenant_id = sqlc.arg(tenant_id)::bigint
  AND c.status IN ('EFFECTIVE', 'EXECUTING')
  -- 收完的不再出现在催收清单上。留 0.01 的容差是因为汇路手续费：
  -- 客户汇的 50000 到账 49975，fee_amount 补上差额后总和可能有分位尾差。
  AND (coalesce(v.total_amount, 0) - coalesce(r.received, 0)) > 0.01
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

-- name: BackfillReceivableDue :execrows
-- 存量补算：已经生效但没有到期日的合同，按传入的（客户 → 账期）补。
-- 幂等，只碰为空的行。
UPDATE contracts SET receivable_due_date = (effective_at::date + sqlc.arg(payment_days)::int)
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
  AND customer_id = sqlc.arg(customer_id)::bigint
  AND receivable_due_date IS NULL
  AND effective_at IS NOT NULL
  AND sqlc.arg(payment_days)::int > 0;

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
    d.title, d.content, '/receivable-due'
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
  AND c.receivable_due_date IS NOT NULL
  -- 没有负责人就没有收件人。这类合同在清单页上仍然看得见，只是没人被点名。
  AND c.sales_employee_id > 0
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
