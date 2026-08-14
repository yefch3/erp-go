-- +goose Up

-- Finished goods go directly from the supplier into port/terminal custody.
ALTER TABLE warehouses DROP CONSTRAINT warehouses_wh_type_check;
ALTER TABLE warehouses ADD CONSTRAINT warehouses_wh_type_check
    CHECK (wh_type IN ('NORMAL', 'BONDED', 'TRANSIT', 'VIRTUAL', 'PORT_TERMINAL'));

-- Convert only the untouched bootstrap location. User-maintained locations
-- are deliberately left alone.
UPDATE warehouses
SET name = '默认码头库', wh_type = 'PORT_TERMINAL'
WHERE code = 'WH01' AND name = '主仓库' AND wh_type = 'NORMAL';

-- +goose Down
UPDATE warehouses
SET name = '主仓库', wh_type = 'NORMAL'
WHERE code = 'WH01' AND name = '默认码头库' AND wh_type = 'PORT_TERMINAL';

ALTER TABLE warehouses DROP CONSTRAINT warehouses_wh_type_check;
ALTER TABLE warehouses ADD CONSTRAINT warehouses_wh_type_check
    CHECK (wh_type IN ('NORMAL', 'BONDED', 'TRANSIT', 'VIRTUAL'));
