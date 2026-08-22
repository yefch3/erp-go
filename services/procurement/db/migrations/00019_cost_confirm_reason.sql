-- +goose Up

-- Why THIS combination of quotes won (A2 收尾). Confirming a cost scenario
-- is the award decision — often not the cheapest row, because MOQ, lead
-- time or an old relationship outweighed price. The operator and timestamp
-- were already recorded; the reasoning was not, and "why did we pick the
-- dearer mill" is exactly the question somebody asks eight months later.
ALTER TABLE cost_scenarios
    ADD COLUMN confirm_reason TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE cost_scenarios DROP COLUMN confirm_reason;
