-- +goose Up
-- Keep source identities and historical sourcing references. Revision is an
-- optimistic concurrency token, never another user-facing business state.
ALTER TABLE sourcing_cases ADD COLUMN inquiry_body JSONB;
ALTER TABLE sourcing_cases ADD COLUMN inquiry_revision BIGINT NOT NULL DEFAULT 1;
ALTER TABLE sourcing_cases ADD COLUMN inquiry_submitted_at TIMESTAMPTZ;
CREATE TABLE inquiry_quotes (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    case_id BIGINT NOT NULL REFERENCES sourcing_cases(id),
    inquiry_revision BIGINT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('PROCUREMENT','LOGISTICS')),
    body JSONB NOT NULL,
    created_by BIGINT NOT NULL,
    created_by_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    submitted_at TIMESTAMPTZ,
    submitted_by BIGINT,
    updated_by BIGINT NOT NULL,
    updated_by_name TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    version BIGINT NOT NULL DEFAULT 1
);
CREATE INDEX inquiry_quotes_case_idx ON inquiry_quotes(tenant_id,case_id,kind);
-- No inferred migration of ambiguous historical saved/submitted quotes.
-- Existing sourcing rows and all contract snapshots are left intact.
-- +goose Down
DROP TABLE inquiry_quotes;
ALTER TABLE sourcing_cases DROP COLUMN inquiry_submitted_at;
ALTER TABLE sourcing_cases DROP COLUMN inquiry_revision;
ALTER TABLE sourcing_cases DROP COLUMN inquiry_body;
