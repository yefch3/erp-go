-- +goose Up

-- 应收到期提醒（E1 第二期）。清单页解决了「记不住」，提醒解决的是
-- 「不用记得去打开」——财务不该每天早上想起来点一次才知道谁逾期了。
--
-- 表的形状照船期那套（shipping_arrival_reminders）：一行是一条已发出的
-- 站内信，唯一键负责幂等。差别在触发方式：
--
-- 船期按「ETA 前 7 天」一个点触发；应收有四个时机（30 天前、7 天前、
-- 当天、逾期后每 7 天一轮）。**判断用范围而不是等号**：写成
-- 「current_date = due_date - 30」的话，worker 那天没跑（重启、故障、
-- 时区跨日）就永远错过那一档；写成范围加唯一键，停三天再起来能一次
-- 补齐，且不会重复发。
CREATE TABLE receivable_reminders (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    contract_id BIGINT NOT NULL REFERENCES contracts(id),
    contract_no VARCHAR(50) NOT NULL,
    customer_name VARCHAR(200) NOT NULL DEFAULT '',
    recipient_employee_id BIGINT NOT NULL,
    -- SOON  到期前 30 天进入视野
    -- DUE   到期前 7 天内（含当天）
    -- OVERDUE 逾期，按 period_no 每 7 天一轮
    reminder_type VARCHAR(16) NOT NULL
        CHECK (reminder_type IN ('SOON', 'DUE', 'OVERDUE')),
    -- 逾期的第几轮（1 = 逾期第一周）。到期前的两档恒为 0。
    period_no INT NOT NULL DEFAULT 0 CHECK (period_no >= 0),
    -- 触发时那张合同的到期日。带进唯一键是因为它可能被补算改过——
    -- 日子换了就该重新提醒一轮，而不是沿用旧日子那条记录当作已提醒。
    due_date DATE NOT NULL,
    -- 发出时的欠款快照。之后收了钱不改这条历史：提醒说的是当时的事实。
    open_amount NUMERIC(18,2) NOT NULL,
    currency VARCHAR(8) NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    detail_url TEXT NOT NULL DEFAULT '/receivable-due',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at TIMESTAMPTZ,
    UNIQUE (tenant_id, contract_id, recipient_employee_id, reminder_type, period_no, due_date)
);

-- 收件箱查询：某人的未读在前、新的在前。
CREATE INDEX receivable_reminders_inbox_idx
    ON receivable_reminders (tenant_id, recipient_employee_id, created_at DESC);

-- +goose Down
DROP TABLE receivable_reminders;
