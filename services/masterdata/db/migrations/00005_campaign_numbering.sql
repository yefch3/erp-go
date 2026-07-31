-- +goose Up
-- Bulk sends draw their own series. Monthly, because the question an export
-- desk asks about a mailshot is "which one did we send in March" — a running
-- yearly number answers nothing.
INSERT INTO number_rules (tenant_id, biz_type, prefix, period, seq_len) VALUES
  (1, 'CAMPAIGN', 'EM', 'MONTHLY', 4);

-- +goose Down
DELETE FROM number_rules WHERE tenant_id = 1 AND biz_type = 'CAMPAIGN';
