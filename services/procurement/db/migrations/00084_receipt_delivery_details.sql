-- +goose Up
CREATE TABLE purchase_receipt_delivery_details (
    receipt_id BIGINT PRIMARY KEY REFERENCES purchase_receipts(id) ON DELETE CASCADE,
    tenant_id BIGINT NOT NULL,
    po_id BIGINT NOT NULL REFERENCES purchase_orders(id),
    actual_delivery_date DATE NOT NULL,
    request_key TEXT NOT NULL DEFAULT '',
    request_hash TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX purchase_receipt_delivery_request_idx ON purchase_receipt_delivery_details(tenant_id,po_id,request_key) WHERE request_key<>'';
CREATE TABLE purchase_receipt_proof_files (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    po_id BIGINT NOT NULL REFERENCES purchase_orders(id),
    receipt_id BIGINT NOT NULL REFERENCES purchase_receipts(id) ON DELETE CASCADE,
    file_name TEXT NOT NULL,
    content_type TEXT NOT NULL,
    file_data BYTEA NOT NULL,
    size_bytes BIGINT NOT NULL
);
CREATE INDEX purchase_receipt_proof_order_idx ON purchase_receipt_proof_files(tenant_id,po_id,receipt_id);

-- +goose Down
DROP TABLE purchase_receipt_proof_files;
DROP TABLE purchase_receipt_delivery_details;
