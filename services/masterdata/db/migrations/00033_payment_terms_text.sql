-- +goose Up
-- migration-safety: widening free-form payment terms to TEXT preserves values
-- and remains compatible with old binaries, which read/write strings.
ALTER TABLE customers
  ALTER COLUMN payment_term TYPE TEXT;

ALTER TABLE suppliers
  ALTER COLUMN payment_term TYPE TEXT;

-- +goose Down
-- Do not shrink columns or truncate terms saved after this migration.
-- Previous binaries can continue using the wider string columns.
SELECT 1;
