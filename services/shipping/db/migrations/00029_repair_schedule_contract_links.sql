-- +goose Up
-- Basic schedule edits previously omitted contract_id and cleared the link.
-- Recover only unambiguous existing handoff references, across every tenant;
-- retain all operational times and audit history.
UPDATE shipping_schedules s
SET contract_id = h.contract_id
FROM contract_shipping_handoffs h
WHERE h.tenant_id = s.tenant_id
  AND h.id = s.contract_handoff_id
  AND h.contract_id > 0
  AND s.contract_id IS NULL
  AND s.contract_no = h.contract_no
  AND s.customer_id = h.customer_id;

-- +goose Down
-- Do not recreate missing associations during a code rollback.
SELECT 1;
