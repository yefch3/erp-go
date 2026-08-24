-- +goose Up

-- Warehouse codes are stable master-data identifiers. Issue them centrally
-- so two administrators creating a warehouse at the same time cannot choose
-- the same code.
INSERT INTO number_rules (tenant_id, biz_type, prefix, period, seq_len)
VALUES (1, 'WAREHOUSE', 'WH', 'NONE', 4)
ON CONFLICT (tenant_id, biz_type) DO NOTHING;

-- +goose Down
DELETE FROM number_rules WHERE tenant_id = 1 AND biz_type = 'WAREHOUSE';
