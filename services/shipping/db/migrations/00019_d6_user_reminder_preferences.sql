-- +goose Up
CREATE TABLE shipping_user_reminder_preferences (
    tenant_id BIGINT NOT NULL,
    employee_id BIGINT NOT NULL,
    lead_days INT[] NOT NULL DEFAULT ARRAY[7],
    timezone TEXT NOT NULL DEFAULT 'UTC',
    holiday_country_codes TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    calendar_sync_status TEXT NOT NULL DEFAULT 'NOT_CONFIGURED'
      CHECK (calendar_sync_status IN ('NOT_CONFIGURED','SYNCED','STALE','FAILED')),
    last_sync_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    cached_holidays JSONB NOT NULL DEFAULT '{}'::JSONB,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, employee_id)
);

-- +goose Down
DROP TABLE shipping_user_reminder_preferences;
