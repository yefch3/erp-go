-- +goose Up
CREATE TABLE quotation_shipments (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1,
    quotation_id BIGINT NOT NULL REFERENCES quotations(id) ON DELETE CASCADE,
    batch_no INT NOT NULL,
    source_customer_selection_shipment_id BIGINT,
    shipment_group_key VARCHAR(100) NOT NULL DEFAULT '',
    carrier_forwarder VARCHAR(200) NOT NULL DEFAULT '',
    service_option_name VARCHAR(200) NOT NULL DEFAULT '',
    customer_managed BOOLEAN NOT NULL DEFAULT FALSE,
    currency CHAR(3) NOT NULL,
    freight_amount NUMERIC(18,2) NOT NULL DEFAULT 0 CHECK (freight_amount >= 0),
    charge_basis VARCHAR(100) NOT NULL DEFAULT '',
    port_of_loading VARCHAR(100) NOT NULL DEFAULT '',
    port_of_discharge VARCHAR(100) NOT NULL DEFAULT '',
    estimated_departure DATE,
    estimated_arrival DATE,
    valid_until DATE,
    remark TEXT NOT NULL DEFAULT '',
    UNIQUE (quotation_id, batch_no)
);
CREATE INDEX quotation_shipments_quotation_idx ON quotation_shipments (tenant_id, quotation_id);

-- +goose Down
DROP TABLE quotation_shipments;
