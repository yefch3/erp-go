-- +goose Up
ALTER TABLE contract_shipping_handoffs DROP CONSTRAINT contract_shipping_handoffs_status_check;
ALTER TABLE contract_shipping_handoffs ADD CONSTRAINT contract_shipping_handoffs_status_check
  CHECK (status IN ('WAITING_REQUOTE','PENDING_APPROVAL','RETURNED','APPROVED',
                    'CONTRACT_UPLOADED','CONTRACT_VERIFIED','PAYMENT_REQUESTED',
                    'PENDING','SCHEDULED','CUSTOMER_MANAGED','SUPERSEDED'));

ALTER TABLE contract_shipping_handoffs
  ADD COLUMN final_forwarder_id BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN final_forwarder_name VARCHAR(200) NOT NULL DEFAULT '',
  ADD COLUMN actual_carrier_id BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN actual_carrier_name VARCHAR(200) NOT NULL DEFAULT '',
  ADD COLUMN final_service_option VARCHAR(200) NOT NULL DEFAULT '',
  ADD COLUMN final_currency VARCHAR(8) NOT NULL DEFAULT '',
  ADD COLUMN final_freight_amount NUMERIC(18,2) NOT NULL DEFAULT 0 CHECK (final_freight_amount>=0),
  ADD COLUMN final_etd DATE,
  ADD COLUMN final_eta DATE,
  ADD COLUMN payment_terms TEXT NOT NULL DEFAULT '',
  ADD COLUMN forwarder_contract_no VARCHAR(80) NOT NULL DEFAULT '',
  ADD COLUMN approval_instance_id BIGINT,
  ADD COLUMN return_reason TEXT NOT NULL DEFAULT '',
  ADD COLUMN operator_id BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN operator_name VARCHAR(100) NOT NULL DEFAULT '',
  ADD COLUMN signed_contract_key TEXT NOT NULL DEFAULT '',
  ADD COLUMN signed_contract_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN signed_contract_uploaded_at TIMESTAMPTZ,
  ADD COLUMN contract_verified_at TIMESTAMPTZ,
  ADD COLUMN contract_verified_by BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN contract_verified_by_name VARCHAR(100) NOT NULL DEFAULT '',
  ADD COLUMN payment_requested_at TIMESTAMPTZ,
  ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE contract_shipping_handoffs
  DROP COLUMN updated_at, DROP COLUMN payment_requested_at,
  DROP COLUMN contract_verified_by_name, DROP COLUMN contract_verified_by,
  DROP COLUMN contract_verified_at, DROP COLUMN signed_contract_uploaded_at,
  DROP COLUMN signed_contract_name, DROP COLUMN signed_contract_key,
  DROP COLUMN operator_name, DROP COLUMN operator_id, DROP COLUMN return_reason,
  DROP COLUMN approval_instance_id, DROP COLUMN forwarder_contract_no,
  DROP COLUMN payment_terms, DROP COLUMN final_eta, DROP COLUMN final_etd,
  DROP COLUMN final_freight_amount, DROP COLUMN final_currency,
  DROP COLUMN final_service_option, DROP COLUMN actual_carrier_name,
  DROP COLUMN actual_carrier_id, DROP COLUMN final_forwarder_name,
  DROP COLUMN final_forwarder_id;
UPDATE contract_shipping_handoffs SET status='WAITING_REQUOTE'
  WHERE status IN ('PENDING_APPROVAL','RETURNED','APPROVED','CONTRACT_UPLOADED','CONTRACT_VERIFIED','PAYMENT_REQUESTED');
ALTER TABLE contract_shipping_handoffs DROP CONSTRAINT contract_shipping_handoffs_status_check;
ALTER TABLE contract_shipping_handoffs ADD CONSTRAINT contract_shipping_handoffs_status_check
  CHECK (status IN ('WAITING_REQUOTE','PENDING','SCHEDULED','CUSTOMER_MANAGED','SUPERSEDED'));
