-- name: UpsertRate :exec
INSERT INTO fx_rates (base_currency, quote_currency, rate, rate_date, source, created_by, note)
VALUES ('USD', $1, sqlc.arg(rate)::text::numeric, $2, $3, $4, $5)
ON CONFLICT (base_currency, quote_currency, rate_date, source)
DO UPDATE SET rate = EXCLUDED.rate, fetched_at = now(),
              created_by = EXCLUDED.created_by, note = EXCLUDED.note;

-- name: LatestRate :one
SELECT quote_currency, rate::text AS rate, rate_date, source, fetched_at
FROM fx_rates
WHERE quote_currency = $1
ORDER BY rate_date DESC, fetched_at DESC
LIMIT 1;

-- name: ListRates :many
SELECT quote_currency, rate::text AS rate, rate_date, source, fetched_at
FROM fx_rates
WHERE quote_currency = $1 AND rate_date >= $2
ORDER BY rate_date DESC, fetched_at DESC;

-- name: InsertAnomaly :exec
INSERT INTO fx_anomalies (quote_currency, new_rate, prev_rate, deviation_pct, source, note)
VALUES ($1, sqlc.arg(new_rate)::text::numeric, sqlc.arg(prev_rate)::text::numeric, sqlc.arg(deviation_pct)::text::numeric, $2, $3);

-- name: ListAnomalies :many
SELECT id, quote_currency, new_rate::text AS new_rate, prev_rate::text AS prev_rate,
       deviation_pct::text AS deviation_pct, source, detected_at, note
FROM fx_anomalies
ORDER BY detected_at DESC
LIMIT 100;
