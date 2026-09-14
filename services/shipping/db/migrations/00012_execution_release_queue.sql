-- +goose Up
-- A finance-released contract is visible to Logistics, but remains fenced
-- before D4 re-quotation and final forwarder selection.
ALTER TABLE contract_shipping_handoffs DROP CONSTRAINT contract_shipping_handoffs_status_check;
ALTER TABLE contract_shipping_handoffs ADD CONSTRAINT contract_shipping_handoffs_status_check
  CHECK (status IN ('WAITING_REQUOTE','PENDING','SCHEDULED','CUSTOMER_MANAGED','SUPERSEDED'));

-- +goose Down
UPDATE contract_shipping_handoffs SET status='PENDING' WHERE status='WAITING_REQUOTE';
ALTER TABLE contract_shipping_handoffs DROP CONSTRAINT contract_shipping_handoffs_status_check;
ALTER TABLE contract_shipping_handoffs ADD CONSTRAINT contract_shipping_handoffs_status_check
  CHECK (status IN ('PENDING','SCHEDULED','CUSTOMER_MANAGED','SUPERSEDED'));
