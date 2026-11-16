-- +goose Up
CREATE TABLE purchase_order_follow_ups (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    po_id BIGINT NOT NULL REFERENCES purchase_orders(id),
    note TEXT NOT NULL CHECK (length(btrim(note)) BETWEEN 1 AND 4000),
    created_by_id BIGINT NOT NULL,
    created_by_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX purchase_order_follow_ups_order_idx ON purchase_order_follow_ups(tenant_id, po_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE purchase_order_follow_ups;
