-- +goose Up
DROP INDEX purchase_execution_assignments_order_idx;
CREATE UNIQUE INDEX purchase_execution_assignments_order_idx
    ON purchase_execution_assignments (tenant_id, po_id, requirement_id) WHERE po_id IS NOT NULL;

-- +goose Down
DROP INDEX purchase_execution_assignments_order_idx;
CREATE UNIQUE INDEX purchase_execution_assignments_order_idx
    ON purchase_execution_assignments (tenant_id, po_id) WHERE po_id IS NOT NULL;
