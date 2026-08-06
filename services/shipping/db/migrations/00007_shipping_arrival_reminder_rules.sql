-- +goose Up
CREATE TABLE shipping_arrival_reminder_rules (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    schedule_id BIGINT NOT NULL REFERENCES shipping_schedules(id) ON DELETE CASCADE,
    lead_days INT NOT NULL CHECK (lead_days BETWEEN 0 AND 3650),
    created_by BIGINT NOT NULL DEFAULT 0,
    created_by_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, schedule_id, lead_days)
);

INSERT INTO shipping_arrival_reminder_rules (tenant_id, schedule_id, lead_days)
SELECT tenant_id, id, 7 FROM shipping_schedules;

ALTER TABLE shipping_arrival_reminders
    DROP CONSTRAINT shipping_arrival_reminders_reminder_type_check;
ALTER TABLE shipping_arrival_reminders
    ADD CONSTRAINT shipping_arrival_reminders_reminder_type_check
        CHECK (reminder_type ~ '^ARRIVAL_[0-9]+D$');

-- +goose Down
-- 回滚仅移除规则配置；历史提醒必须保留，因此兼容多天数的类型约束不收窄。
DROP TABLE shipping_arrival_reminder_rules;
