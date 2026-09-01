-- +goose Up
CREATE TABLE contract_shipping_handoffs (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    contract_id BIGINT NOT NULL,
    contract_no VARCHAR(50) NOT NULL,
    contract_version_id BIGINT NOT NULL,
    version_no INT NOT NULL,
    customer_id BIGINT NOT NULL DEFAULT 0,
    customer_name VARCHAR(200) NOT NULL DEFAULT '',
    batch_no INT NOT NULL,
    shipment_group_key VARCHAR(100) NOT NULL DEFAULT '',
    carrier_forwarder VARCHAR(200) NOT NULL DEFAULT '',
    service_option_name VARCHAR(200) NOT NULL DEFAULT '',
    customer_managed BOOLEAN NOT NULL DEFAULT FALSE,
    currency CHAR(3) NOT NULL,
    freight_amount NUMERIC(18,2) NOT NULL DEFAULT 0,
    charge_basis VARCHAR(100) NOT NULL DEFAULT '',
    port_of_loading VARCHAR(100) NOT NULL DEFAULT '',
    port_of_discharge VARCHAR(100) NOT NULL DEFAULT '',
    estimated_departure DATE,
    estimated_arrival DATE,
    valid_until DATE,
    remark TEXT NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'PENDING'
      CHECK (status IN ('PENDING','SCHEDULED','CUSTOMER_MANAGED','SUPERSEDED')),
    schedule_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, contract_version_id, batch_no)
);
CREATE INDEX contract_shipping_handoffs_pending_idx ON contract_shipping_handoffs (tenant_id, status, created_at DESC);

ALTER TABLE shipping_schedules ADD COLUMN contract_handoff_id BIGINT REFERENCES contract_shipping_handoffs(id);
CREATE UNIQUE INDEX shipping_schedules_handoff_unique ON shipping_schedules (tenant_id, contract_handoff_id) WHERE contract_handoff_id IS NOT NULL;

CREATE TABLE processed_events (
    event_id VARCHAR(200) NOT NULL,
    consumer_group VARCHAR(100) NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, consumer_group)
);

-- +goose Down
DROP TABLE processed_events;
DROP INDEX shipping_schedules_handoff_unique;
ALTER TABLE shipping_schedules DROP COLUMN contract_handoff_id;
DROP TABLE contract_shipping_handoffs;
