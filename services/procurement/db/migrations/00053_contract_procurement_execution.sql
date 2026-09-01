-- +goose Up
-- T9：合同生效后的代采购任务必须保留供应商最终复询的商务条件。
ALTER TABLE purchase_requirements
  ADD COLUMN source_payment_terms VARCHAR(120) NOT NULL DEFAULT '',
  ADD COLUMN source_incoterm VARCHAR(40) NOT NULL DEFAULT '',
  ADD COLUMN source_valid_until DATE;

-- +goose Down
ALTER TABLE purchase_requirements
  DROP COLUMN source_valid_until,
  DROP COLUMN source_incoterm,
  DROP COLUMN source_payment_terms;
