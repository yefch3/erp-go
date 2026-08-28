-- +goose Up

-- Keep the durable master-data relationship as well as the historical name/email
-- snapshots. Existing inquiries predate contact selection and remain unlinked.
ALTER TABLE sourcing_cases
    ADD COLUMN contact_id BIGINT NOT NULL DEFAULT 0;

CREATE INDEX sourcing_cases_contact_idx
    ON sourcing_cases (tenant_id, customer_id, contact_id)
    WHERE contact_id > 0;

-- +goose Down

DROP INDEX IF EXISTS sourcing_cases_contact_idx;
ALTER TABLE sourcing_cases DROP COLUMN IF EXISTS contact_id;
