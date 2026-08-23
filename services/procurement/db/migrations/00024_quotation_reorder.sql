-- +goose Up

-- 同一份客户报价、同一家供应商，允许再开一张采购单（A5 补购）。
--
-- 00022 建这条唯一索引，为的是拦住重复点击下单。它确实拦住了，但连正当的
-- 补购一起拦了：工厂这批只能供 80 吨，剩下的 20 吨过些天再向同一家追加，
-- 是这行生意里的常事——系统给的回答却是「该客户报价与供应商已经生成采购
-- 单」，路到此为止。
--
-- 防重复点击的活儿本来就不该由这条索引干。真正的把关在两处：下单事务开头
-- 会把采购需求整行锁住，并且不允许下单量超过「还能下多少」。重复提交同一
-- 笔数量，第二次必然撞在这道闸上。索引只是多此一举地把「以后再也不能向这
-- 家买」也一起写死了。
--
-- 改法：索引降级成普通索引（下单前查一次现有单子还要用它），把判断挪到
-- 应用层——还没确认的草稿仍然当重复处理，已经下过的单则要求写明为什么再
-- 来一张。
DROP INDEX IF EXISTS purchase_orders_quotation_supplier_idx;

CREATE INDEX purchase_orders_quotation_supplier_idx
    ON purchase_orders (tenant_id, source_quotation_id, supplier_id)
    WHERE source_quotation_id > 0 AND status <> 'CANCELLED';

-- +goose Down

-- 回滚会在已经存在补购单的库上失败——那正是想要的：唯一索引和补购不能
-- 并存，与其悄悄丢掉一批单子，不如让回滚停下来。
DROP INDEX IF EXISTS purchase_orders_quotation_supplier_idx;

CREATE UNIQUE INDEX purchase_orders_quotation_supplier_idx
    ON purchase_orders (tenant_id, source_quotation_id, supplier_id)
    WHERE source_quotation_id > 0 AND status <> 'CANCELLED';
