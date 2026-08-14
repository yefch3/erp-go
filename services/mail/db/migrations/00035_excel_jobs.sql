-- +goose Up
CREATE TABLE mail_excel_jobs (
    id                BIGSERIAL PRIMARY KEY,
    tenant_id         BIGINT NOT NULL,
    owner_id          BIGINT NOT NULL,
    inbound_id        BIGINT NOT NULL,
    attachment_id     BIGINT,
    selected_text     TEXT,
    locale            VARCHAR(10) NOT NULL DEFAULT 'zh',
    status            VARCHAR(20) NOT NULL DEFAULT 'PENDING'
                      CHECK (status IN ('PENDING', 'PROCESSING', 'COMPLETED', 'FAILED')),
    attempt_count     INT NOT NULL DEFAULT 0,
    file_name         TEXT NOT NULL DEFAULT '',
    file_data         BYTEA,
    workbook_json     JSONB,
    model             TEXT NOT NULL DEFAULT '',
    error_code        VARCHAR(100) NOT NULL DEFAULT '',
    error_message     TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at        TIMESTAMPTZ,
    completed_at      TIMESTAMPTZ,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((attachment_id IS NOT NULL) <> (selected_text IS NOT NULL))
);

CREATE INDEX mail_excel_jobs_claim_idx
    ON mail_excel_jobs (status, created_at, id)
    WHERE status IN ('PENDING', 'PROCESSING');
CREATE INDEX mail_excel_jobs_owner_idx
    ON mail_excel_jobs (tenant_id, owner_id, id DESC);

-- +goose Down
DROP TABLE mail_excel_jobs;
