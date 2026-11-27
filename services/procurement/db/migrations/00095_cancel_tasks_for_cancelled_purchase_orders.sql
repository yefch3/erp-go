-- +goose Up
UPDATE purchase_execution_assignments a
SET cancelled = true
FROM purchase_orders o
WHERE a.tenant_id = o.tenant_id AND a.po_id = o.id
  AND o.status = 'CANCELLED' AND NOT a.cancelled;

-- +goose Down
-- Cancelled orders remain cancelled on rollback, so their tasks stay closed.
SELECT 1;
