-- +goose Up
CREATE TABLE shipping_operational_alerts (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    schedule_id BIGINT NOT NULL REFERENCES shipping_schedules(id) ON DELETE CASCADE,
    alert_type TEXT NOT NULL CHECK (alert_type IN ('ETA_ADVANCED','ETD_DELAYED','WAREHOUSE_DELAYED')),
    recipient_employee_id BIGINT NOT NULL,
    recipient_role TEXT NOT NULL,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    old_value TEXT NOT NULL DEFAULT '',
    new_value TEXT NOT NULL DEFAULT '',
    due_date DATE,
    read_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    resolution_note TEXT NOT NULL DEFAULT '',
    resolved_by BIGINT,
    resolved_by_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, schedule_id, alert_type, recipient_employee_id, new_value)
);
CREATE INDEX shipping_operational_alert_recipient_idx
    ON shipping_operational_alerts (tenant_id, recipient_employee_id, resolved_at, created_at DESC);

-- +goose Down
DROP TABLE shipping_operational_alerts;
