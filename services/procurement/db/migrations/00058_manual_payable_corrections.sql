-- +goose Up
-- Finance-only opening records may need identifying details corrected later.
-- Keep an append-only before/after record without rewriting payment history.
CREATE TABLE manual_payable_corrections (
    id                BIGSERIAL PRIMARY KEY,
    tenant_id         BIGINT NOT NULL,
    po_id             BIGINT NOT NULL REFERENCES purchase_orders(id),
    before_data       JSONB NOT NULL,
    after_data        JSONB NOT NULL,
    corrected_by      BIGINT NOT NULL,
    corrected_by_name VARCHAR(200) NOT NULL DEFAULT '',
    corrected_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX manual_payable_corrections_po_idx
    ON manual_payable_corrections (tenant_id, po_id, corrected_at DESC);

-- +goose Down
DROP TABLE manual_payable_corrections;
