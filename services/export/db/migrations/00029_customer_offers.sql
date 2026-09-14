-- +goose Up
CREATE TABLE customer_offers (
 tenant_id BIGINT NOT NULL,
 case_id BIGINT NOT NULL,
 revision BIGINT NOT NULL DEFAULT 1,
 body JSONB NOT NULL,
 source_snapshot JSONB NOT NULL,
 quotation_id BIGINT REFERENCES quotations(id),
 contract_id BIGINT REFERENCES contracts(id),
 updated_by BIGINT NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 confirmed_at TIMESTAMPTZ,
 PRIMARY KEY(tenant_id,case_id)
);
-- +goose Down
DROP TABLE customer_offers;
