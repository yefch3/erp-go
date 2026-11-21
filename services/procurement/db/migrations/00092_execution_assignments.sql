-- +goose Up
CREATE TABLE purchase_execution_assignments (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    requirement_id BIGINT NOT NULL REFERENCES purchase_requirements(id),
    quote_id BIGINT NOT NULL REFERENCES purchase_execution_supplier_quotes(id),
    buyer_id BIGINT NOT NULL,
    buyer_name TEXT NOT NULL,
    quantity NUMERIC(20,6) NOT NULL CHECK (quantity > 0),
    instructions TEXT NOT NULL,
    assigned_by BIGINT NOT NULL,
    changed_by BIGINT NOT NULL,
    negotiated_price NUMERIC(20,6) CHECK (negotiated_price > 0),
    expected_date DATE,
    payment_terms TEXT NOT NULL DEFAULT '',
    remark TEXT NOT NULL DEFAULT '',
    po_id BIGINT REFERENCES purchase_orders(id) ON DELETE SET NULL,
    cancelled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX purchase_execution_assignments_requirement_idx
    ON purchase_execution_assignments (tenant_id, requirement_id) WHERE NOT cancelled;
CREATE INDEX purchase_execution_assignments_buyer_idx
    ON purchase_execution_assignments (tenant_id, buyer_id) WHERE NOT cancelled;
CREATE UNIQUE INDEX purchase_execution_assignments_order_idx
    ON purchase_execution_assignments (tenant_id, po_id) WHERE po_id IS NOT NULL;

CREATE TABLE purchase_execution_assignment_files (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    assignment_id BIGINT NOT NULL REFERENCES purchase_execution_assignments(id) ON DELETE CASCADE,
    file_name TEXT NOT NULL,
    content_type TEXT NOT NULL,
    file_data BYTEA NOT NULL,
    uploaded_by_id BIGINT NOT NULL,
    uploaded_by_name TEXT NOT NULL
);
CREATE INDEX purchase_execution_assignment_files_assignment_idx
    ON purchase_execution_assignment_files (tenant_id, assignment_id);

-- +goose Down
DROP TABLE purchase_execution_assignment_files;
DROP TABLE purchase_execution_assignments;
