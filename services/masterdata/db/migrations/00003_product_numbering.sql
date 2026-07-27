-- +goose Up
-- Product codes are issued by the same numbering service as customers and
-- suppliers: one owner for every document number in the system.
INSERT INTO number_rules (tenant_id, biz_type, prefix, period, seq_len) VALUES
  (1, 'PRODUCT', 'P', 'NONE', 5);

-- +goose Down
DELETE FROM number_rules WHERE tenant_id = 1 AND biz_type = 'PRODUCT';
