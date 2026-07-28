-- +goose Up

-- Files that belong to a contract: the drafted PDF, the copy the customer
-- signed and sent back, and whatever else gets attached along the way.
--
-- Bound to a VERSION, not just the contract. A change means a new version and
-- a fresh signature, so "which paper did they actually sign" is only
-- answerable if each file remembers the terms it was signed against.
CREATE TABLE contract_attachments (
    id                  BIGSERIAL     PRIMARY KEY,
    tenant_id           BIGINT        NOT NULL DEFAULT 1,
    contract_id         BIGINT        NOT NULL REFERENCES contracts(id),
    contract_version_id BIGINT        REFERENCES contract_versions(id),
    kind                VARCHAR(32)   NOT NULL DEFAULT 'OTHER'
                        CHECK (kind IN ('DRAFT','SIGNED','OTHER')),
    file_name           VARCHAR(300)  NOT NULL,
    -- Object storage key. The bucket is configuration, so it is not stored.
    file_key            VARCHAR(500)  NOT NULL,
    content_type        VARCHAR(200)  NOT NULL DEFAULT '',
    size_bytes          BIGINT        NOT NULL DEFAULT 0,
    uploaded_at         TIMESTAMPTZ   NOT NULL DEFAULT now(),
    uploaded_by         BIGINT        NOT NULL DEFAULT 0,
    uploader_name       VARCHAR(100)  NOT NULL DEFAULT '',
    -- One row per object; a repeated register is a duplicate, not a second file.
    UNIQUE (tenant_id, file_key)
);
CREATE INDEX contract_attachments_contract_idx
    ON contract_attachments (tenant_id, contract_id, uploaded_at DESC);

-- +goose Down
DROP TABLE contract_attachments;
