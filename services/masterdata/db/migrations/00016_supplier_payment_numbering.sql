-- +goose Up
-- Payment slips get numbered like every other document. Monthly reset, four
-- digits, PAY prefix — the same shape as purchase orders, because the people
-- reading these numbers off bank confirmations are the same people.
INSERT INTO number_rules (tenant_id, biz_type, prefix, period, seq_len)
VALUES (1, 'SUPPLIER_PAYMENT', 'PAY', 'MONTHLY', 4)
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM number_rules WHERE biz_type = 'SUPPLIER_PAYMENT';
