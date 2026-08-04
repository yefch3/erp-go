-- +goose Up

CREATE SEQUENCE shipping_schedule_no_seq;

CREATE TABLE shipping_schedule_changes (
    id          BIGSERIAL   PRIMARY KEY,
    tenant_id   BIGINT      NOT NULL DEFAULT 1,
    schedule_id BIGINT      NOT NULL REFERENCES shipping_schedules(id),
    change_type VARCHAR(20) NOT NULL
        CHECK (change_type IN ('DATE','STATUS','CANCEL')),
    field_name  VARCHAR(32) NOT NULL,
    old_value   TEXT        NOT NULL DEFAULT '',
    new_value   TEXT        NOT NULL DEFAULT '',
    reason      TEXT        NOT NULL DEFAULT '',
    operator_id BIGINT      NOT NULL,
    operator_name VARCHAR(100) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX shipping_schedule_changes_schedule_idx
    ON shipping_schedule_changes (tenant_id, schedule_id, created_at DESC);

-- +goose Down
DROP TABLE shipping_schedule_changes;
DROP SEQUENCE shipping_schedule_no_seq;
