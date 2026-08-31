-- +goose Up

-- migration-safety: DROP COLUMN purchase_orders.payment_days ——
--   从来没有任何查询点名过它。00040 把它加进来时只写进 INSERT/UPDATE，
--   唯一的读取方是 SetPurchaseOrderOrdered 里那句 CASE，而那句已在 00043
--   随「到期日改成单据自己的字段」一起删掉。全仓 grep 确认：
--   services/procurement/db/queries/ 下没有一条查询提到它（只剩两行注释），
--   purchase_orders 上也没有 SELECT *（唯一的星号在 sourcing_shipping.sql，
--   查的是另一张表）。所以旧容器回滚回来也读不到它，没有向后兼容问题。
--   2026-08-30 确认。
--
-- 对照：customers / suppliers 上的同名列**不能这样删**，那两张表的
-- CreateCustomer / GetSupplier 等八条查询原本是 SELECT *，sqlc 展开之后
-- 会把 payment_days 点名写进 SQL。那八条已在本次改成显式清单，等这一版
-- 上线之后，下一版才轮到删列。
ALTER TABLE purchase_orders DROP COLUMN payment_days;

-- +goose Down

ALTER TABLE purchase_orders ADD COLUMN payment_days INT NOT NULL DEFAULT 0;
