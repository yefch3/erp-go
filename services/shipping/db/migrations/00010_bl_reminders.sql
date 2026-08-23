-- +goose Up

-- 提单签发提醒（E2）。业务的原话是「船出发了以后，要提醒船务部门，
-- 签提单要及时，整个部门的」。
--
-- 为什么不复用 shipping_arrival_reminders：那张表和「到港」绑得很紧
-- （外键指向目的港节点、worker 里 JOIN 出到港文案、状态机为发邮件而设）。
-- 硬塞进去会让那个函数变成一堆 if；而这件事的形状其实和 E1 的应收提醒
-- 一样——一条 SQL 扫出该提醒的，写成站内信，幂等交给唯一键。
--
-- **一人一行，不是一条发给部门。** 部门是收件人的来源，不是收件人本身。
-- 展开成多行之后，「谁读了、谁还没读」才是可回答的问题；发一条挂在部门
-- 名下，等于谁都以为别人会处理。
CREATE TABLE shipping_bl_reminders (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    schedule_id BIGINT NOT NULL REFERENCES shipping_schedules(id),
    -- 快照：提醒说的是发出那一刻的事实，之后船名改了不改这条历史。
    schedule_no VARCHAR(50) NOT NULL DEFAULT '',
    vessel_name VARCHAR(200) NOT NULL DEFAULT '',
    voyage_no VARCHAR(100) NOT NULL DEFAULT '',
    contract_no VARCHAR(50) NOT NULL DEFAULT '',
    customer_name VARCHAR(200) NOT NULL DEFAULT '',
    recipient_employee_id BIGINT NOT NULL,
    -- 开船后第几轮（每 3 天一轮）。1 = 开船后第 3–5 天那一轮。
    period_no INT NOT NULL CHECK (period_no >= 1),
    -- 触发时用的开船日。改了 ETD/ATD 就该重新起一轮，所以它进唯一键。
    departed_on DATE NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    detail_url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at TIMESTAMPTZ,
    UNIQUE (tenant_id, schedule_id, recipient_employee_id, period_no, departed_on)
);

CREATE INDEX shipping_bl_reminders_inbox_idx
    ON shipping_bl_reminders (tenant_id, recipient_employee_id, created_at DESC);

-- +goose Down
DROP TABLE shipping_bl_reminders;
