-- +goose Up
ALTER TABLE contract_shipping_handoffs DROP CONSTRAINT contract_shipping_handoffs_status_check;
ALTER TABLE contract_shipping_handoffs ADD CONSTRAINT contract_shipping_handoffs_status_check
  CHECK (status IN ('WAITING_REQUOTE','DRAFT','PENDING_APPROVAL','RETURNED','APPROVED',
                    'CONTRACT_UPLOADED','CONTRACT_VERIFIED','PAYMENT_REQUESTED',
                    'PENDING','SCHEDULED','CUSTOMER_MANAGED','SUPERSEDED'));

-- +goose Down
UPDATE contract_shipping_handoffs SET status='WAITING_REQUOTE' WHERE status='DRAFT';
ALTER TABLE contract_shipping_handoffs DROP CONSTRAINT contract_shipping_handoffs_status_check;
ALTER TABLE contract_shipping_handoffs ADD CONSTRAINT contract_shipping_handoffs_status_check
  CHECK (status IN ('WAITING_REQUOTE','PENDING_APPROVAL','RETURNED','APPROVED',
                    'CONTRACT_UPLOADED','CONTRACT_VERIFIED','PAYMENT_REQUESTED',
                    'PENDING','SCHEDULED','CUSTOMER_MANAGED','SUPERSEDED'));
