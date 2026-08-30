-- +goose Up
ALTER TABLE supplier_quotes
    ADD COLUMN incoterm VARCHAR(80) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE supplier_quotes
    DROP COLUMN incoterm;
