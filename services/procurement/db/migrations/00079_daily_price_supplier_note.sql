-- +goose Up
ALTER TABLE daily_price_dimensions ADD COLUMN note VARCHAR(200) NOT NULL DEFAULT '';
UPDATE daily_price_dimensions
SET note = CASE WHEN name IN ('东钢', '纵横', '新天钢', '智融') THEN '出厂价'
                WHEN name = '神龙' THEN '鲅鱼圈'
                WHEN name = '澳森' THEN '天津港' ELSE '' END
WHERE kind = 'SUPPLIER' AND master_id IS NULL AND note = ''
  AND name IN ('东钢', '纵横', '新天钢', '智融', '神龙', '澳森');

-- +goose Down
ALTER TABLE daily_price_dimensions DROP COLUMN note;
