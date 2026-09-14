-- +goose Up
-- D3: Finance records why an executing contract may be released. The existing
-- audit columns keep who/when/note; this column keeps the required four-way
-- business decision instead of hiding it in free text.
ALTER TABLE contracts
  ADD COLUMN execution_condition_type VARCHAR(40) NOT NULL DEFAULT ''
  CHECK (execution_condition_type IN (
    '', 'PREPAYMENT_RECEIVED', 'LETTER_OF_CREDIT_RECEIVED',
    'NO_PREPAYMENT_REQUIRED', 'SPECIAL_APPROVAL'
  ));

-- +goose Down
ALTER TABLE contracts DROP COLUMN execution_condition_type;
