-- +goose Up
CREATE TABLE fx_watch_currencies (
  tenant_id bigint NOT NULL,
  currency char(3) NOT NULL,
  sort_order integer NOT NULL DEFAULT 0,
  created_by bigint NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(tenant_id,currency)
);
-- +goose Down
DROP TABLE fx_watch_currencies;
