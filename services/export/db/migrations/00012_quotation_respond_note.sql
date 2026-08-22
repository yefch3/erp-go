-- +goose Up

-- What the customer actually said (B1). "REJECTED" alone teaches nothing;
-- "too expensive by 5 USD/t" is the input the next cost scenario is built
-- from. Kept on the quotation because that is where the answer arrived.
ALTER TABLE quotations
    ADD COLUMN respond_note TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE quotations DROP COLUMN respond_note;
