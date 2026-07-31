-- +goose Up

-- Stock gains a value, not just a quantity.
--
-- Until now nothing in this service knew what anything cost, so the question
-- every finance function starts from — "what was the cost of the goods we
-- just shipped" — had no answer at all. A purchase order carried a unit price
-- and the receipt threw it away.
--
-- Moving weighted average, because it is the method that needs no month-end
-- machinery to be correct: the average is right after every single movement,
-- which suits a system whose ledger is already append-only and read live.
-- Standard cost with variance accounts is the other common choice and needs a
-- costing run and variance postings — that belongs with the general ledger,
-- not before it.
ALTER TABLE stocks
    -- One cost pool, one currency. Mixing currencies in a single average
    -- produces a number that means nothing, so a receipt in a different
    -- currency is refused rather than silently averaged in.
    ADD COLUMN cost_currency VARCHAR(8) NOT NULL DEFAULT 'CNY',
    ADD COLUMN total_cost NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (total_cost >= 0),
    -- Derived, so no code path can compute it differently. Six decimals
    -- because a unit cost divided out of a large receipt needs more precision
    -- than the money it will eventually post.
    ADD COLUMN avg_cost NUMERIC(18,6)
        GENERATED ALWAYS AS (
            CASE WHEN on_hand_qty > 0 THEN total_cost / on_hand_qty ELSE 0 END
        ) STORED;

-- Every movement records what it was worth, so the ledger reconciles in money
-- as well as in quantity. Without this a wrong average could be spotted but
-- never traced back to the movement that caused it.
ALTER TABLE stock_ledger
    ADD COLUMN unit_cost      NUMERIC(18,6) NOT NULL DEFAULT 0,
    ADD COLUMN amount         NUMERIC(18,4) NOT NULL DEFAULT 0,
    -- The average after this movement, which is what makes the running cost
    -- auditable: the same reason on_hand_after and available_after are here.
    ADD COLUMN avg_cost_after NUMERIC(18,6) NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE stock_ledger
    DROP COLUMN avg_cost_after,
    DROP COLUMN amount,
    DROP COLUMN unit_cost;
ALTER TABLE stocks
    DROP COLUMN avg_cost,
    DROP COLUMN total_cost,
    DROP COLUMN cost_currency;
