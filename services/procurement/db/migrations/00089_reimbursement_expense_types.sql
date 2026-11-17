-- +goose Up
ALTER TABLE travel_reimbursements
  ADD COLUMN IF NOT EXISTS expense_type text NOT NULL DEFAULT 'TRAVEL'
    CHECK (expense_type IN ('TRAVEL', 'OFFICE_SUPPLIES', 'OTHER')),
  ADD COLUMN IF NOT EXISTS expense_date date,
  ALTER COLUMN trip_start DROP NOT NULL,
  ALTER COLUMN trip_end DROP NOT NULL;

-- +goose Down
-- Keep additive fields and nullable dates so rolling back application code cannot
-- delete expense classifications or invent travel dates for non-travel claims.
SELECT 1;
