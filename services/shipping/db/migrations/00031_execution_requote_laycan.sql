-- +goose Up
-- Historical ETD/ETA are not Laycan dates. Keep the old values as-is.
ALTER TABLE shipping_execution_requote_options
  ALTER COLUMN etd DROP NOT NULL,
  ALTER COLUMN eta DROP NOT NULL,
  ALTER COLUMN payment_terms SET DEFAULT '',
  ADD COLUMN laycan_start date,
  ADD COLUMN laycan_end date,
  ADD CONSTRAINT shipping_requote_laycan_range CHECK (
    (laycan_start IS NULL AND laycan_end IS NULL) OR
    (laycan_start IS NOT NULL AND laycan_end IS NOT NULL AND laycan_start <= laycan_end)
  );

ALTER TABLE contract_shipping_handoffs
  ADD COLUMN laycan_start date,
  ADD COLUMN laycan_end date,
  ADD CONSTRAINT shipping_handoff_laycan_range CHECK (
    (laycan_start IS NULL AND laycan_end IS NULL) OR
    (laycan_start IS NOT NULL AND laycan_end IS NOT NULL AND laycan_start <= laycan_end)
  );

-- +goose Down
-- Once new quotes exist, this rollback fails instead of inventing ETD/ETA.
ALTER TABLE shipping_execution_requote_options
  ALTER COLUMN etd SET NOT NULL,
  ALTER COLUMN eta SET NOT NULL,
  ALTER COLUMN payment_terms DROP DEFAULT,
  DROP COLUMN laycan_start,
  DROP COLUMN laycan_end;
ALTER TABLE contract_shipping_handoffs
  DROP COLUMN laycan_start,
  DROP COLUMN laycan_end;
