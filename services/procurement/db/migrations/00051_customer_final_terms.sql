-- +goose Up
-- The offline customer decision freezes the commercial terms Sales actually
-- agreed with the customer. These are deliberately separate from the
-- supplier recheck terms because Sales may negotiate different customer terms.
ALTER TABLE sourcing_customer_selection_items
  ADD COLUMN final_customer_payment_terms VARCHAR(120) NOT NULL DEFAULT '',
  ADD COLUMN final_customer_incoterm VARCHAR(40) NOT NULL DEFAULT '',
  ADD COLUMN final_customer_required_date DATE;

-- +goose Down
ALTER TABLE sourcing_customer_selection_items
  DROP COLUMN final_customer_required_date,
  DROP COLUMN final_customer_incoterm,
  DROP COLUMN final_customer_payment_terms;
