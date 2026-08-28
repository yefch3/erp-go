-- +goose Up
-- migration-safety: widening quotation text columns is backward compatible —
-- the previous binary reads them as strings and keeps writing values within
-- its old limits. The wider source-side values arrive only through this release.
ALTER TABLE quotations
  ALTER COLUMN incoterm TYPE VARCHAR(80),
  ALTER COLUMN port_of_loading TYPE VARCHAR(150),
  ALTER COLUMN port_of_discharge TYPE VARCHAR(150),
  ALTER COLUMN payment_method TYPE VARCHAR(200);

ALTER TABLE quotation_items
  ALTER COLUMN spec TYPE TEXT,
  ALTER COLUMN uom_code TYPE VARCHAR(50);

-- +goose Down
-- A safe rollback must not truncate customer quotation snapshots that already
-- use the widened limits. Keeping the wider columns is compatible with old code.
SELECT 1;
