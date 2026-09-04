-- +goose Up
-- Optional reference for people to find the paper behind a bank row. It is
-- plain text on purpose: no automatic matching and no extra status.
ALTER TABLE bank_transactions
  ADD COLUMN document_no VARCHAR(100) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE bank_transactions DROP COLUMN document_no;
