-- +goose Up
-- Shipping documents draw their own series. Distinct from SHIPMENT_PLAN, which
-- numbers the plan to ship; this numbers the boat that actually took it.
-- Monthly, matching contracts: an export desk looks things up by the month
-- they sailed, and a year-long running number tells nobody anything.
INSERT INTO number_rules (tenant_id, biz_type, prefix, period, seq_len) VALUES
  (1, 'SHIPMENT', 'SH', 'MONTHLY', 4);

-- +goose Down
DELETE FROM number_rules WHERE tenant_id = 1 AND biz_type = 'SHIPMENT';
