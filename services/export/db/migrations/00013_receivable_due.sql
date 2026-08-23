-- +goose Up

-- 应收到期日（E1）。业务定的口径：**账期从合同生效日起算**——收到定金/
-- 预付款/信用证之后才去下采购单，所以合同生效本身就意味着首付已经到位，
-- 之后的 120/150 天算的是尾款。
--
-- 为什么落库而不是每次现算（effective_at + 客户当前账期）：客户的账期会
-- 改，而改账期不该动已经发生的账。今天把张三的账期从 120 天调到 90 天，
-- 半年前那批合同的到期日必须还是当初约定的那个——否则财务一觉醒来发现
-- 一堆单子「突然逾期了」，而没有任何人做过任何事。同一个道理让
-- purchase_order_items 快照了产品名、supplier_invoices 快照了汇率。
--
-- 允许为空，而且**空是有意义的**：客户主数据里没配账期（payment_days=0）
-- 时留空，不假装它当天到期。清单页会把这类合同单独列出来催配置——
-- 「不知道」和「今天到期」是两件事，混在一起财务会被假逾期淹没。
ALTER TABLE contracts ADD COLUMN receivable_due_date DATE;

-- 到期清单的主查询：按到期日排、只看没收完的。逾期的排最前，所以升序。
CREATE INDEX contracts_receivable_due_idx
    ON contracts (tenant_id, receivable_due_date)
    WHERE status IN ('EFFECTIVE', 'EXECUTING') AND receivable_due_date IS NOT NULL;

-- +goose Down
DROP INDEX contracts_receivable_due_idx;
ALTER TABLE contracts DROP COLUMN receivable_due_date;
