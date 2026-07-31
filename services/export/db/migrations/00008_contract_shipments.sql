-- +goose Up

-- What has physically left against a contract.
--
-- A separate table, not a shipped_qty column on contract_items, and that is
-- the whole point: an approved contract version is frozen by trigger because
-- it is the record of what was agreed. What has since shipped is a fact about
-- the world, not about the agreement, and it changes every time a truck
-- leaves. Putting a moving number inside the frozen document would either
-- break the freeze or force the freeze to grow exceptions — and an
-- immutability rule with exceptions stops being one.
CREATE TABLE contract_shipments (
    id               BIGSERIAL     PRIMARY KEY,
    tenant_id        BIGINT        NOT NULL DEFAULT 1,
    contract_id      BIGINT        NOT NULL REFERENCES contracts(id),
    -- References contract_items(id), deliberately without a foreign key: the
    -- line may belong to a version that has since been superseded, and the
    -- shipment against it still happened.
    contract_item_id BIGINT        NOT NULL,
    product_id       BIGINT        NOT NULL DEFAULT 0,
    sku_id           BIGINT        NOT NULL DEFAULT 0,
    outbound_no      VARCHAR(50)   NOT NULL,
    qty              NUMERIC(18,4) NOT NULL CHECK (qty > 0),
    shipped_at       TIMESTAMPTZ   NOT NULL DEFAULT now(),
    -- Idempotency: Kafka promises at-least-once, and the same outbound
    -- redelivered must not count the goods twice.
    UNIQUE (tenant_id, outbound_no, contract_item_id)
);
CREATE INDEX contract_shipments_contract_idx
    ON contract_shipments (tenant_id, contract_id);
CREATE INDEX contract_shipments_item_idx
    ON contract_shipments (tenant_id, contract_item_id);

-- +goose Down
DROP TABLE contract_shipments;
