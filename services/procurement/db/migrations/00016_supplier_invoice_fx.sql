-- +goose Up

-- The invoice's half of the fx story (A4 P6). supplier_payments carried
-- these three columns from day one; the invoice needs the same trio because
-- realized gain/loss is the DIFFERENCE between two snapshots: what the claim
-- was worth in book currency the day it was entered, and what the cash was
-- worth the day it left. One snapshot alone prices nothing.
--
-- Zero means "not captured" (entered before P6, or fx was unreachable at
-- entry) — such rows are excluded from gain/loss rather than pretending.
ALTER TABLE supplier_invoices
    ADD COLUMN base_currency VARCHAR(8)    NOT NULL DEFAULT 'CNY',
    ADD COLUMN base_amount   NUMERIC(18,2) NOT NULL DEFAULT 0,
    ADD COLUMN fx_rate       NUMERIC(18,8) NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE supplier_invoices
    DROP COLUMN base_currency,
    DROP COLUMN base_amount,
    DROP COLUMN fx_rate;
