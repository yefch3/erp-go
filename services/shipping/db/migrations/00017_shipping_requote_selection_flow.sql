-- +goose Up
-- Older D4 builds moved a handoff to DRAFT when the first candidate was saved.
-- Return only incomplete rows to inquiry so the final candidate can be selected.
UPDATE contract_shipping_handoffs
SET status = 'WAITING_REQUOTE', updated_at = now()
WHERE status = 'DRAFT'
  AND final_forwarder_id = 0
  AND final_service_option = '';

-- +goose Down
-- Data correction intentionally has no automatic reverse operation.
