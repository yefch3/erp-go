-- +goose Up
-- Master-data codes are now issued by the numbering service (still editable
-- on the form for companies with their own conventions). NONE period: plain
-- CU-0001 style without a date segment.
INSERT INTO number_rules (tenant_id, biz_type, prefix, period, seq_len) VALUES
  (1, 'CUSTOMER', 'CU', 'NONE', 4),
  (1, 'SUPPLIER', 'SU', 'NONE', 4);

-- +goose Down
DELETE FROM number_rules WHERE biz_type IN ('CUSTOMER','SUPPLIER') AND tenant_id = 1;
