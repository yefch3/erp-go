-- +goose Up

-- Who a document was handed to, by whom, and why.
--
-- The owner column on quotations and contracts only ever says who owns it
-- NOW. Without this table a handover is indistinguishable from a mistake, and
-- a commission dispute a year later has nothing to read.
--
-- One row per document, so a quotation and the contract made from it produce
-- two rows for one handover: the history of a contract has to be answerable
-- without knowing which quotation it came from.
CREATE TABLE ownership_transfers (
    id                  BIGSERIAL PRIMARY KEY,
    tenant_id           BIGINT      NOT NULL,
    biz_type            VARCHAR(20) NOT NULL,   -- QUOTATION / CONTRACT
    biz_id              BIGINT      NOT NULL,
    biz_no              VARCHAR(50) NOT NULL,   -- snapshot: readable without a join
    from_employee_id    BIGINT      NOT NULL,
    from_employee       VARCHAR(50) NOT NULL,
    to_employee_id      BIGINT      NOT NULL,
    to_employee         VARCHAR(50) NOT NULL,
    reason              TEXT        NOT NULL DEFAULT '',
    -- The supervisor who performed it, which is never the same question as
    -- who it went to.
    transferred_by      BIGINT      NOT NULL,
    transferred_by_name VARCHAR(50) NOT NULL,
    transferred_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT ownership_transfers_biz_type_check
        CHECK (biz_type IN ('QUOTATION', 'CONTRACT')),
    -- Handing a document to the person who already holds it is a no-op the
    -- caller should have caught, not a row.
    CONSTRAINT ownership_transfers_distinct_check
        CHECK (from_employee_id <> to_employee_id)
);

CREATE INDEX ownership_transfers_biz_idx
    ON ownership_transfers (tenant_id, biz_type, biz_id, transferred_at DESC);

-- +goose Down
DROP TABLE ownership_transfers;
