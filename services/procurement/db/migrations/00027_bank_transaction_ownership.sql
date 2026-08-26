-- +goose Up

-- 银行流水的「归属」：这一笔钱是谁那条线上的（F2 第一步）。
--
-- 背景：银行对账单是一份，里面进账出账混在一起。在此之前，这张表只能把
-- **出账**匹配到供应商付款单；进账导进来躺着，这张表的建表注释自己写着
-- 「导进来只是为了完整，目前不匹配任何东西」。而客户收款那本账在出口服务
-- 的另一张同名表里，两边互不相识。
--
-- **为什么归属不能用方向推出来**——这是整件事最要紧的一条：
--
--   * 供应商退款（supplier_payments.payment_type='REFUND'）是钱**进**来，
--     却必须对到采购的付款单上
--   * 退款给客户是钱**出**去，却归客户那条线
--
-- 所以归属是一个独立的字段，不是 direction 的同义词。原来那条「只有出账
-- 才能匹配付款单」的限制正是踩在这个坑上：凡是退过款的供应商，供应商对账
-- 那一行的欠款余额都是多算的——退回来的钱没地方记。
--
-- 五档（含空）：
--
--   ''           待处理。默认值，也是导进来的样子——**不猜**
--   CUSTOMER     客户收款  → 去对出口合同
--   SUPPLIER     供应商往来 → 去对采购付款单（含退款，进账也算）
--   TAX_REFUND   出口退税  → 先只记着，功能以后做
--   OTHER        不用核销  → 银行利息、内部划转、保证金退回…
--
-- OTHER 这一档不是凑数：银行对账单里天天有利息和手续费，没有它，人会被逼着
-- 把这些硬塞进「客户收款」——**那比空着更糟**，因为空着至少是诚实的。
ALTER TABLE bank_transactions
    ADD COLUMN ownership VARCHAR(16) NOT NULL DEFAULT ''
        CHECK (ownership IN ('', 'CUSTOMER', 'SUPPLIER', 'TAX_REFUND', 'OTHER')),
    -- OTHER 的二级分类，沿用出口那张表已有的取值（少掉 SUPPLIER_REFUND，
    -- 它现在归 SUPPLIER，不再是「与我无关」）。其余归属下留空。
    ADD COLUMN ownership_detail VARCHAR(32) NOT NULL DEFAULT '';

-- 已经被某张付款单认领的流水，归属就是供应商——这是**事实推出来的**，
-- 不是猜的：有付款单指着它，它当然是供应商那条线上的。
--
-- 其余的一律留空（待处理）。哪怕出账十有八九是付供应商的，也不替人填：
-- 一个猜出来的归属会让人以为已经有人看过这一笔了。
UPDATE bank_transactions b SET ownership = 'SUPPLIER'
 WHERE EXISTS (
     SELECT 1 FROM supplier_payments p
      WHERE p.bank_txn_id = b.id AND p.tenant_id = b.tenant_id
 );

-- 列表默认筛「待处理」，所以这条路要走得动。
CREATE INDEX bank_transactions_ownership_idx
    ON bank_transactions (tenant_id, ownership, txn_date DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS bank_transactions_ownership_idx;
ALTER TABLE bank_transactions
    DROP COLUMN ownership_detail,
    DROP COLUMN ownership;
