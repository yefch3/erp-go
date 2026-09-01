-- +goose Up

-- 流水可以删了，而「删」是**归档**，不是抹掉。
--
-- 起因是登记流水这一步没有回头路：填错一行——日期打错、金额多一个零、
-- 同一笔录了两遍——就一直挂在列表里，只能靠备注写一句「作废」。
--
-- 做成软删除而不是真删，有三个理由，第三个是最硬的：
--
--  1. 银行流水是账。一行进过账又消失了，事后没人说得清它是错录的还是被
--     谁抹掉的——而「说不清」正是这种表最该避免的状态。
--  2. 删要填理由，而理由要有地方待着。真删了，理由跟着一起没。
--  3. **UNIQUE (tenant_id, bank_ref) 还在。** 真删之后重新导入同一份对账单，
--     那一行会重新长回来——于是「删除」在下一次导入时被静默撤销。留着行，
--     那条唯一键就继续挡住重复导入，删除也就真的算数。
--
-- 另外：supplier_payments.bank_txn_id 还有存量数据指着这里（那是「把付款单
-- 对上流水」年代的东西，那个功能已经下线，只剩一个取消匹配的口子）。**没有
-- 外键**，所以真删会留下一堆指向不存在行的悬空引用。归档则一行都不悬空。
ALTER TABLE bank_transactions
    ADD COLUMN deleted_at      TIMESTAMPTZ,
    ADD COLUMN deleted_by_id   BIGINT       NOT NULL DEFAULT 0,
    ADD COLUMN deleted_by_name VARCHAR(100) NOT NULL DEFAULT '',
    -- 为什么删。必填——一条没有理由的删除记录，和真删了没多大区别：
    -- 事后翻到它，还是不知道当时发生了什么。
    ADD COLUMN delete_reason   TEXT         NOT NULL DEFAULT '';

-- 列表默认只看没删的，而列表是按日期倒序翻页的。原来那条
-- bank_transactions_list_idx 不带 deleted_at，加了过滤条件之后它只能先扫
-- 再筛——删掉的越多扫得越冤。
--
-- 部分索引而不是把 deleted_at 加进原索引：列表要的是「活着的那些」，
-- 已删除那个页面是另一条路，量小得多，走顺序扫都行。
CREATE INDEX bank_transactions_live_list_idx
    ON bank_transactions (tenant_id, txn_date DESC, id DESC)
    WHERE deleted_at IS NULL;

-- +goose Down

DROP INDEX IF EXISTS bank_transactions_live_list_idx;
ALTER TABLE bank_transactions
    DROP COLUMN delete_reason,
    DROP COLUMN deleted_by_name,
    DROP COLUMN deleted_by_id,
    DROP COLUMN deleted_at;
