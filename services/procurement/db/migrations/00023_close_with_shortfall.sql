-- +goose Up

-- 货没到齐也能关单（A5）。
--
-- 场景：订了 100 吨，工厂只能给 80 吨，也确实只发了 80 吨。在此之前这单
-- 永远关不掉——关单要求「全部收满」，而剩下的 20 吨永远不会来。货收完了、
-- 钱付了、事情办完了，系统却一直在等。财务和采购只能在系统外记一笔
-- 「这单其实完了」。
--
-- 关单时记两件事：少收了多少、剩下的怎么处理。
--
-- 「订了 100」是事实，「认 80 就算完」是判断——所以订单明细的数量一个字
-- 不改，判断另记。同 A4「付款是事实、核销是判断」、A3「收货是事实、
-- 结不结案是判断」。
ALTER TABLE purchase_orders
    ADD COLUMN close_note TEXT NOT NULL DEFAULT '',
    -- 关单时没收到的那部分怎么办：
    --   ''         全部收齐了，没有这个问题
    --   REORDER    放回待采购清单，另找工厂（业务说这是常态）
    --   DROPPED    不要了（客户减了量，或这批就这样了）
    ADD COLUMN shortfall_action VARCHAR(16) NOT NULL DEFAULT ''
        CHECK (shortfall_action IN ('', 'REORDER', 'DROPPED'));

-- +goose Down
ALTER TABLE purchase_orders
    DROP COLUMN shortfall_action,
    DROP COLUMN close_note;
