-- +goose Up
CREATE TABLE effective_rates (
  tenant_id BIGINT NOT NULL,
  base_currency TEXT NOT NULL CHECK (base_currency ~ '^[A-Z]{3}$'),
  quote_currency TEXT NOT NULL CHECK (quote_currency ~ '^[A-Z]{3}$'),
  rate NUMERIC(24,8) NOT NULL CHECK (rate > 0),
  confirmed_by_id BIGINT NOT NULL,
  confirmed_by TEXT NOT NULL,
  confirmed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  remark TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (tenant_id, base_currency, quote_currency),
  CHECK (base_currency <> quote_currency)
);
-- +goose Down
DROP TABLE effective_rates;
