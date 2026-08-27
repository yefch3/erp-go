-- +goose Up

-- 「这一行处理完了没有」是**账本自己的状态**，不是某一条线的私事。
--
-- 两条线各自有认领的方式，但问的是同一个问题：
--
--   归属 SUPPLIER  一张供应商付款单认领它（匹配即全额认领）
--   归属 CUSTOMER  一条或多条核销记录认领它（可以分几次核完）
--
-- 有了这一列，「还没处理完」对两条线就是同一个定义：claimed_amount < amount。
-- 财务每天要清的那个队列，于是能在一次查询里分页筛出来。
--
-- 没有它的话，收款对账的「待处理 / 已核销」筛选就得把核销记录从出口库拖过来
-- 再在内存里筛——那样分页就不准了，第二页会漏行。
--
-- **谁来写这一列**：供应商那条线由本服务在匹配/取消匹配时写；客户那条线由出口
-- 服务在核销/冲销之后通过 gRPC 写回来。出口写的是**它自己的判断的结果**，不是
-- 往别人家塞私货：认领了多少，本来就是账本该知道的事。
--
-- **万一出口那次写回失败了**：这一列会偏旧，那一行可能停在错误的筛选档里。
-- 但**金额不会错**——收款对账每次读都用出口自己的核销记录重新算一遍显示的
-- 数，这一列只决定「出现在哪个筛选里」。宁可筛错档，不可显示错数。

ALTER TABLE bank_transactions
    ADD COLUMN claimed_amount NUMERIC(18,2) NOT NULL DEFAULT 0
        CHECK (claimed_amount >= 0);

-- 按事实回填：已经被某张付款单认领的行，就是全额认领。
-- 没被认领的一律留 0——猜一个数会让人以为已经有人处理过这一笔了。
UPDATE bank_transactions b SET claimed_amount = b.amount
 WHERE EXISTS (SELECT 1 FROM supplier_payments p
                WHERE p.bank_txn_id = b.id AND p.tenant_id = b.tenant_id);

-- 队列查询走这条：某一档归属里还没认领完的，按日子倒着排。
CREATE INDEX bank_transactions_open_idx
    ON bank_transactions (tenant_id, ownership, txn_date DESC, id DESC)
 WHERE claimed_amount < amount;

-- +goose Down
DROP INDEX bank_transactions_open_idx;
ALTER TABLE bank_transactions DROP COLUMN claimed_amount;
