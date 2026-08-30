-- +goose Up

-- 应付到期日：这张采购单的钱什么时候该付出去。
--
-- 和客户侧的 contracts.receivable_due_date 同一个形状。口径由业务定：
-- **账期从下单那天起算**（客户那边是从合同生效那天起算）。
ALTER TABLE purchase_orders
    -- 下单时供应商的付款账期，快照。
    --
    -- **快照而不是每次去主数据查**，理由和这张表上 supplier_name /
    -- supplier_code / currency 那几列完全一样：这张单是按当时谈定的条件
    -- 下的，供应商事后改账期不该把已经下出去的单一起改掉。
    ADD COLUMN payment_days INT NOT NULL DEFAULT 0,
    -- 到期日。**为空 = 没配账期**，不是「今天到期」。
    --
    -- 下单那一刻算一次（ordered_at + payment_days），此后不再变。为空的单
    -- 在对账页上显示成「未配账期」，有一个专门的筛子挑出来补——编一个日子
    -- 会让「今天该付谁」这句话变成假的。
    ADD COLUMN payable_due_date DATE;

-- 对账页按到期日排队要走这条：只有真正下出去过的单才谈得上到期。
CREATE INDEX purchase_orders_payable_due_idx
    ON purchase_orders (tenant_id, payable_due_date)
 WHERE payable_due_date IS NOT NULL;

-- **存量数据不在这里回填。** 迁移够不着 masterdata（账期在另一个服务的库
-- 里），而且此刻供应商的 payment_days 全是 0——这一列是同一批改动里刚加的。
-- 存量单一律显示「未配账期」，等供应商那边配好之后走 BackfillPayableDue
-- 补算，和客户侧 BackfillReceivableDue 同一套路。

-- +goose Down
DROP INDEX IF EXISTS purchase_orders_payable_due_idx;
ALTER TABLE purchase_orders
    DROP COLUMN IF EXISTS payable_due_date,
    DROP COLUMN IF EXISTS payment_days;
