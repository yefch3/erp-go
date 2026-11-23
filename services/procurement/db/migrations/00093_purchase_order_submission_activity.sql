-- +goose Up
CREATE TABLE purchase_order_submission_activity (
    tenant_id BIGINT NOT NULL,
    po_id BIGINT NOT NULL REFERENCES purchase_orders(id),
    approval_instance_id BIGINT NOT NULL,
    operator_id BIGINT NOT NULL,
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, po_id, approval_instance_id)
);
CREATE INDEX purchase_order_submission_activity_day_idx
    ON purchase_order_submission_activity (tenant_id, submitted_at);

-- +goose Down
DROP TABLE purchase_order_submission_activity;
