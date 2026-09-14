-- +goose Up
CREATE TABLE shipping_execution_requote_options (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  handoff_id BIGINT NOT NULL REFERENCES contract_shipping_handoffs(id) ON DELETE CASCADE,
  forwarder_id BIGINT NOT NULL,
  forwarder_name VARCHAR(200) NOT NULL,
  actual_carrier_id BIGINT NOT NULL DEFAULT 0,
  actual_carrier_name VARCHAR(200) NOT NULL DEFAULT '',
  service_option VARCHAR(200) NOT NULL,
  currency VARCHAR(8) NOT NULL,
  freight_amount NUMERIC(18,2) NOT NULL CHECK (freight_amount > 0),
  etd DATE NOT NULL,
  eta DATE NOT NULL,
  payment_terms TEXT NOT NULL,
  remark TEXT NOT NULL DEFAULT '',
  created_by BIGINT NOT NULL DEFAULT 0,
  created_by_name VARCHAR(100) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX shipping_execution_requote_options_handoff_idx
  ON shipping_execution_requote_options (tenant_id, handoff_id, updated_at DESC, id DESC);

-- Preserve drafts created before multiple candidates were introduced.
INSERT INTO shipping_execution_requote_options (
  tenant_id,handoff_id,forwarder_id,forwarder_name,actual_carrier_id,actual_carrier_name,
  service_option,currency,freight_amount,etd,eta,payment_terms,remark,created_by,created_by_name
)
SELECT tenant_id,id,final_forwarder_id,final_forwarder_name,actual_carrier_id,actual_carrier_name,
  final_service_option,final_currency,final_freight_amount,final_etd,final_eta,payment_terms,remark,
  operator_id,operator_name
FROM contract_shipping_handoffs
WHERE status IN ('DRAFT','RETURNED') AND final_forwarder_id > 0
  AND final_freight_amount > 0 AND final_etd IS NOT NULL AND final_eta IS NOT NULL;

-- +goose Down
DROP TABLE shipping_execution_requote_options;
