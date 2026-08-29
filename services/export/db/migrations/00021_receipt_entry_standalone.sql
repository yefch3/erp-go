-- +goose Up

-- 核销记录不再必须挂一条银行流水。
--
-- 需求变了：收款核销从「拿一行银行流水去认合同」改成「选一张合同，金额由
-- 员工手填」。银行流水降级成纯记录（那本账在采购库里，只用来存银行给的
-- statement），核销这边一根线都不连。
--
-- 三件加法，一件删减都没有：老行原样留着继续算数。**这是选择就地扩列而不
-- 是新建表的全部理由**——「这张合同收了多少」= sum(amount + fee_amount)
-- 这句话在出口库里被复制了六遍（合同收款进度、可核销合同下拉、按合同号找
-- 合同、应收到期清单、催收扫描、合同执行看板），新建表意味着这六处全改再
-- 加双写；就地扩列则一处都不用动，老行和新行天然一起求和。

-- 1. 放开「必须挂流水」。00016 当时只删了外键（跨库不能有外键），没动
--    NOT NULL——那是核销记录对银行流水仅存的一处结构性耦合。
ALTER TABLE receipt_allocations ALTER COLUMN transaction_id DROP NOT NULL;

-- 2. 这笔钱哪天到的。原来这个日子从银行流水行上读（value_date），现在由
--    员工填。**老行留空而不是补今天**：那个日子在采购库的流水行上，这里
--    编一个出来就是撒谎。
ALTER TABLE receipt_allocations ADD COLUMN received_at DATE;

-- 3. 员工写的一句话（水单号、"分两笔中的第一笔"、"客户说尾款下月"）。
--    和银行给的备注不是一回事，那个在流水行上。
ALTER TABLE receipt_allocations ADD COLUMN note TEXT NOT NULL DEFAULT '';

-- 4. 「正常收完」这一档。
--
--    这张表原来四档（LOSS 认亏 / ROUNDING 尾差 / CANCELLED 合同取消 /
--    OTHER 其他）全是「算式说还欠、人说不欠了」的理由——因为老模型里收满
--    是算式自动消失的，根本走不到这张表。新模型下**完成与否完全由人确认**，
--    最常见的完成理由恰恰是「收齐了，正常结案」，四档里一个都套不上。
ALTER TABLE contract_receivable_closures DROP CONSTRAINT contract_receivable_closures_category_check;
ALTER TABLE contract_receivable_closures ADD CONSTRAINT contract_receivable_closures_category_check
    CHECK (category IN ('SETTLED', 'LOSS', 'ROUNDING', 'CANCELLED', 'OTHER'));

-- +goose Down

-- 回滚方向不是纯粹的镜像，两处要先把新数据"折叠"回旧形状，否则约束加不回去。
UPDATE contract_receivable_closures SET category = 'OTHER' WHERE category = 'SETTLED';
ALTER TABLE contract_receivable_closures DROP CONSTRAINT contract_receivable_closures_category_check;
ALTER TABLE contract_receivable_closures ADD CONSTRAINT contract_receivable_closures_category_check
    CHECK (category IN ('LOSS', 'ROUNDING', 'CANCELLED', 'OTHER'));

ALTER TABLE receipt_allocations DROP COLUMN note;
ALTER TABLE receipt_allocations DROP COLUMN received_at;
-- 不挂流水的核销行在旧形状里无处安放，用 0 占位（0 号流水不存在，
-- 恰好等同于"没有流水"）。
UPDATE receipt_allocations SET transaction_id = 0 WHERE transaction_id IS NULL;
ALTER TABLE receipt_allocations ALTER COLUMN transaction_id SET NOT NULL;
