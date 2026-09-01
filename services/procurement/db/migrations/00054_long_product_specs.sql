-- +goose Up
-- 客户询盘中的完整规格可能超过 300 字符。采购需求与采购单都必须保存
-- 这份冻结快照，不能在合同生效或生成采购单时截断。
-- migration-safety: ALTER COLUMN spec TYPE TEXT — 仅放宽 PostgreSQL 字符串列；旧版本仍按 string 扫描和写入，兼容 varchar 与 text。
ALTER TABLE purchase_requirements
  ALTER COLUMN spec TYPE TEXT;

ALTER TABLE purchase_order_items
  ALTER COLUMN spec TYPE TEXT;

-- +goose Down
ALTER TABLE purchase_order_items
  ALTER COLUMN spec TYPE VARCHAR(300);

ALTER TABLE purchase_requirements
  ALTER COLUMN spec TYPE VARCHAR(300);
