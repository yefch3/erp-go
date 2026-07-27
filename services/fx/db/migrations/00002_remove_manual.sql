-- +goose Up
-- Business decision 2026-07-27: manual rate entry removed; the API feed is
-- the only source. Existing manual test rows are purged; anomaly history
-- stays as audit.
DELETE FROM fx_rates WHERE source = 'MANUAL';
ALTER TABLE fx_rates DROP CONSTRAINT fx_rates_source_check;
ALTER TABLE fx_rates ADD CONSTRAINT fx_rates_source_check CHECK (source IN ('FRANKFURTER'));

-- +goose Down
ALTER TABLE fx_rates DROP CONSTRAINT fx_rates_source_check;
ALTER TABLE fx_rates ADD CONSTRAINT fx_rates_source_check CHECK (source IN ('FRANKFURTER','MANUAL'));
