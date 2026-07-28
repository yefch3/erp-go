-- +goose Up

-- Two additions to a flow version.
--
-- 1. min_amount turns "one flow per document type" into "one flow per amount
--    band". A band is identified by its FLOOR alone - there is no max_amount
--    column, because a ceiling that is stored separately from the next band's
--    floor can drift into gaps ("nothing matches 100,000") and overlaps
--    ("two flows match"). Selection is "the highest floor at or below the
--    amount", so neither is expressible.
--
-- 2. created_by, because whoever edits a flow can route any document to
--    themselves and approve it. An action that can bypass every approval in
--    the system should not be the one action with no author on it.
ALTER TABLE approval_definitions
    ADD COLUMN min_amount NUMERIC(18,2) NOT NULL DEFAULT 0,
    ADD COLUMN created_by BIGINT        NOT NULL DEFAULT 0;

-- Versions now count per band rather than per document type.
ALTER TABLE approval_definitions
    DROP CONSTRAINT approval_definitions_tenant_id_biz_type_version_key;
ALTER TABLE approval_definitions
    ADD CONSTRAINT approval_definitions_band_version_key
    UNIQUE (tenant_id, biz_type, min_amount, version);

CREATE INDEX approval_definitions_band_idx
    ON approval_definitions (tenant_id, biz_type, min_amount DESC, version DESC)
    WHERE status = 'ACTIVE';

-- The amount the flow was chosen by, kept on the instance so the timeline can
-- explain why a document took the route it did. The engine treats it as an
-- opaque scalar: it does not know this is money, only that it is comparable.
ALTER TABLE approval_instances ADD COLUMN amount NUMERIC(18,2) NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE approval_instances DROP COLUMN amount;
DROP INDEX approval_definitions_band_idx;
ALTER TABLE approval_definitions DROP CONSTRAINT approval_definitions_band_version_key;
ALTER TABLE approval_definitions
    ADD CONSTRAINT approval_definitions_tenant_id_biz_type_version_key
    UNIQUE (tenant_id, biz_type, version);
ALTER TABLE approval_definitions DROP COLUMN created_by, DROP COLUMN min_amount;
