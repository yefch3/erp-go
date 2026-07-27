-- +goose Up
-- Market rates are world facts shared by every tenant: deliberately
-- tenant-free (listed in scripts/check-tenant-id.sh EXEMPT).
-- rate = units of quote currency per 1 USD (1 USD = 7.24 CNY -> 7.24).
CREATE TABLE fx_rates (
    id             BIGSERIAL PRIMARY KEY,
    base_currency  CHAR(3)       NOT NULL DEFAULT 'USD',
    quote_currency CHAR(3)       NOT NULL,
    rate           NUMERIC(18,8) NOT NULL CHECK (rate > 0),
    rate_date      DATE          NOT NULL,
    source         VARCHAR(20)   NOT NULL CHECK (source IN ('FRANKFURTER','MANUAL')),
    created_by     BIGINT        NOT NULL DEFAULT 0,
    note           TEXT          NOT NULL DEFAULT '',
    fetched_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (base_currency, quote_currency, rate_date, source)
);
CREATE INDEX fx_rates_pair_idx ON fx_rates (quote_currency, rate_date DESC);

CREATE TABLE fx_anomalies (
    id             BIGSERIAL PRIMARY KEY,
    quote_currency CHAR(3)       NOT NULL,
    new_rate       NUMERIC(18,8) NOT NULL,
    prev_rate      NUMERIC(18,8) NOT NULL,
    deviation_pct  NUMERIC(8,4)  NOT NULL,
    source         VARCHAR(20)   NOT NULL,
    detected_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    note           TEXT          NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE fx_anomalies;
DROP TABLE fx_rates;
