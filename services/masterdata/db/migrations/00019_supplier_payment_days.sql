-- +goose Up

-- 供应商的付款账期，单位天。
--
-- 和 customers.payment_days（00008）同一个形状、同一个用途：应付到期日
-- 是「下单当天 + 这个天数」，客户那边是「合同生效当天 + 天数」。
--
-- **和已有的 payment_term 不重复。** 那一列是一段自由文本（「T/T 30 days」
-- 这种），给人看的；算不出日子。这一列是它的数值补充，专给到期日的算术用。
-- 两列都留着：合同上怎么写是一回事，系统按哪个数排队是另一回事。
--
-- **0 表示没配，不是「当天到期」。** 采购单在下单那一刻拿这个数算到期日，
-- 是 0 就把到期日留空——对账页上显示成「未配账期」，用一个专门的筛子挑出来
-- 补。编一个日子会让「今天该付谁」这句话变成假的。
ALTER TABLE suppliers
    ADD COLUMN payment_days INT NOT NULL DEFAULT 0;

ALTER TABLE suppliers
    ADD CONSTRAINT suppliers_payment_days_check CHECK (payment_days >= 0);

-- +goose Down
ALTER TABLE suppliers DROP CONSTRAINT IF EXISTS suppliers_payment_days_check;
ALTER TABLE suppliers DROP COLUMN IF EXISTS payment_days;
