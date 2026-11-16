-- +goose Up
-- migration-safety: widening payment terms and their snapshots to TEXT is
-- backward compatible with previous binaries, which read/write strings.
-- No downstream snapshot may be narrower than a contract or supplier quote.
ALTER TABLE supplier_quotes
  ALTER COLUMN payment_terms TYPE TEXT;

ALTER TABLE procurement_plan_items
  ALTER COLUMN payment_terms TYPE TEXT;

ALTER TABLE sourcing_final_recheck_tasks
  ALTER COLUMN final_payment_terms TYPE TEXT;

ALTER TABLE sourcing_customer_selection_items
  ALTER COLUMN final_customer_payment_terms TYPE TEXT;

ALTER TABLE purchase_requirements
  ALTER COLUMN source_payment_terms TYPE TEXT;

-- +goose Down
-- Keep long payment terms intact if the application is rolled back.
-- Previous binaries can continue reading/writing these string columns.
SELECT 1;
