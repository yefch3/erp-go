-- +goose Up
-- migration-safety: widening payment terms to TEXT preserves existing values
-- and remains compatible with previous binaries, which read/write strings.
-- Direct contracts accept free-form terms; quotation-to-contract conversion
-- must preserve the same text without a narrower destination column.
ALTER TABLE contract_versions
  ALTER COLUMN payment_method TYPE TEXT;

ALTER TABLE quotations
  ALTER COLUMN payment_method TYPE TEXT;

-- +goose Down
-- Keep the wider columns on rollback: shrinking them would reject or truncate
-- payment terms already saved by this release. Old binaries can read TEXT.
SELECT 1;
