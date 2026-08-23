-- name: CreateBankAccount :one
INSERT INTO bank_accounts (tenant_id, account_no, account_name, bank_name, currency)
VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(account_no)::text,
    sqlc.arg(account_name)::text,
    sqlc.arg(bank_name)::text,
    sqlc.arg(currency)::text
)
RETURNING id;

-- name: ListBankAccounts :many
SELECT id, account_no, account_name, bank_name, currency, status
FROM bank_accounts
WHERE tenant_id = sqlc.arg(tenant_id)::bigint
ORDER BY status, id;

-- name: RecordBankTransaction :one
INSERT INTO bank_transactions (
    tenant_id, account_id, bank_ref, direction, amount, currency, value_date,
    counterparty, counterparty_account, remittance_info, source, trusted_ref,
    note, recorded_by, recorded_by_name
) VALUES (
    sqlc.arg(tenant_id)::bigint,
    sqlc.arg(account_id)::bigint,
    sqlc.arg(bank_ref)::text,
    sqlc.arg(direction)::text,
    sqlc.arg(amount)::text::numeric,
    sqlc.arg(currency)::text,
    sqlc.arg(value_date)::text::date,
    sqlc.arg(counterparty)::text,
    sqlc.arg(counterparty_account)::text,
    sqlc.arg(remittance_info)::text,
    sqlc.arg(source)::text,
    sqlc.arg(trusted_ref)::text,
    sqlc.arg(note)::text,
    sqlc.arg(recorded_by)::bigint,
    sqlc.arg(recorded_by_name)::text
)
RETURNING id;

-- name: GetBankTransaction :one
SELECT
    t.id, t.account_id, t.bank_ref, t.direction,
    t.amount::text AS amount, t.currency,
    t.value_date::text AS value_date,
    t.counterparty, t.counterparty_account, t.remittance_info,
    t.source, t.trusted_ref, t.disposition, t.irrelevant_type, t.note,
    t.recorded_by_name, t.created_at,
    coalesce(a.account_no, '')::text   AS account_no,
    coalesce(a.account_name, '')::text AS account_name,
    coalesce(x.allocated, 0)::text     AS allocated_amount,
    (t.amount - coalesce(x.allocated, 0))::text AS unallocated_amount,
    coalesce(x.fees, 0)::text          AS fee_amount
FROM bank_transactions t
LEFT JOIN bank_accounts a ON a.id = t.account_id
LEFT JOIN (
    SELECT transaction_id, sum(amount) AS allocated, sum(fee_amount) AS fees
    FROM receipt_allocations
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
    GROUP BY transaction_id
) x ON x.transaction_id = t.id
WHERE t.tenant_id = sqlc.arg(tenant_id)::bigint AND t.id = sqlc.arg(id)::bigint;

-- name: LockBankTransaction :one
SELECT id, bank_ref, direction, amount::text AS amount, currency, disposition
FROM bank_transactions
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint
FOR UPDATE;

-- name: ListBankTransactions :many
-- The reconciliation queue. Unprocessed first is deliberate: this page exists
-- to be emptied, and a list that opens on last month's settled lines is a
-- list nobody works through.
SELECT
    t.id, t.bank_ref, t.direction,
    t.amount::text AS amount, t.currency,
    t.value_date::text AS value_date,
    t.counterparty, t.remittance_info, t.source, t.trusted_ref,
    t.disposition, t.irrelevant_type, t.recorded_by_name, t.created_at,
    coalesce(a.account_name, '')::text AS account_name,
    coalesce(x.allocated, 0)::text     AS allocated_amount,
    (t.amount - coalesce(x.allocated, 0))::text AS unallocated_amount,
    count(*) OVER () AS total
FROM bank_transactions t
LEFT JOIN bank_accounts a ON a.id = t.account_id
LEFT JOIN (
    SELECT transaction_id, sum(amount) AS allocated
    FROM receipt_allocations
    WHERE tenant_id = sqlc.arg(tenant_id)::bigint
    GROUP BY transaction_id
) x ON x.transaction_id = t.id
WHERE t.tenant_id = sqlc.arg(tenant_id)::bigint
  AND (sqlc.arg(disposition)::text = '' OR t.disposition = sqlc.arg(disposition)::text)
  AND (sqlc.arg(direction)::text = '' OR t.direction = sqlc.arg(direction)::text)
  AND (sqlc.arg(keyword)::text = ''
       OR t.bank_ref        ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR t.counterparty    ILIKE '%' || sqlc.arg(keyword)::text || '%'
       OR t.remittance_info ILIKE '%' || sqlc.arg(keyword)::text || '%')
ORDER BY (t.disposition = 'UNPROCESSED') DESC, t.value_date DESC, t.id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

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

-- name: SetTransactionDisposition :execrows
UPDATE bank_transactions SET
    disposition     = sqlc.arg(disposition)::text,
    irrelevant_type = sqlc.arg(irrelevant_type)::text,
    note            = CASE WHEN sqlc.arg(note)::text = '' THEN note ELSE sqlc.arg(note)::text END,
    updated_at      = now()
WHERE tenant_id = sqlc.arg(tenant_id)::bigint AND id = sqlc.arg(id)::bigint;

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
SELECT
    a.id, a.transaction_id, a.amount::text AS amount,
    a.fee_amount::text AS fee_amount, a.currency,
    coalesce(a.reversal_of, 0)::bigint AS reversal_of,
    a.allocated_by_name, a.allocated_at,
    t.bank_ref, t.value_date::text AS value_date, t.counterparty, t.source
FROM receipt_allocations a
JOIN bank_transactions t ON t.id = a.transaction_id
WHERE a.tenant_id = sqlc.arg(tenant_id)::bigint
  AND a.contract_id = sqlc.arg(contract_id)::bigint
ORDER BY t.value_date DESC, a.id DESC;

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
