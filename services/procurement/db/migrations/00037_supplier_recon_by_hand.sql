-- +goose Up

-- 供应商核销改成「列出采购订单、员工手填数字、人工确认完成」。
-- 三件加法，一道既有的守门都没删：老的付款单 → 发票那条路原样留着。
--
-- **为什么是就地扩列，而不是新建一张手工核销表**：
-- 「这张采购单付了多少」= sum(amount) FROM payment_allocations WHERE po_id=?
-- 这句话在采购库里已经写了三遍——退款上限守门（supplierpayment.go:486）、
-- 冲销守门（:627）、供应商往来汇总的预付列（supplierstatement.go:132）。
-- 新建一张表意味着这三处全部改成两表求和，并且从此要双写；就地扩列则一处
-- 都不用动，改造前的预付核销行和改造后的手填行天然一起被求和。

-- 1. 放开「必须挂付款单」。
--    核销行原来必须先有一张 supplier_payments 抬头（付款单号、付款方式、
--    银行 ref）。新模型下员工在采购单那一行上直接填一个数，没有抬头这一层：
--    payment_id 为空，就是「手填的」。
--    这也是核销记录对付款单、进而对银行流水仅存的一处结构性耦合——放开它，
--    「核销记录不留指向流水的线索」就是结构上成立的，不需要再删任何东西。
ALTER TABLE payment_allocations ALTER COLUMN payment_id DROP NOT NULL;

-- 2. 这笔钱哪天付的。原来这个日子在抬头的 paid_at 上；手填行没有抬头，
--    所以下沉到核销行。
--    **老行留空，不从抬头拷一份**：拷贝会立刻分叉（抬头改了、行不改），
--    而老行的日子在它自己的抬头上一直查得到，拷贝换不来任何东西。
ALTER TABLE payment_allocations ADD COLUMN paid_at DATE;

-- 3. 员工自己写的一句话。付款单的 remark 说的是那张付款单，这一列说的是
--    这一笔核销。
ALTER TABLE payment_allocations ADD COLUMN note TEXT NOT NULL DEFAULT '';

-- 手填行的目标只可能是采购单，走的是既有的 CHECK 里 po_id 那一边；
-- payment_alloc_po_idx 已经是 po_id 上的部分索引，不再加索引。

-- 4. 「这张采购单核销完了没有」——由人说出来，并且记下来是谁在什么时候
--    按什么理由说的。
--
-- 完全照客户侧 contract_receivable_closures 的形状（export 00019 建表、
-- 00021 补的 SETTLED 一档），因为那个形状已经承受过一次需求翻面：
--   · open_amount 只存快照、不做任何校验，正负都收——差额是正是负都能确认完成
--   · 数字对上了**不会**自动确认完成，必须有人点
--   · 确认完成之后照样能继续记付款；钱真的又付了就撤销完成
--   · 撤销不删行，revoked_at 一填，「当初是谁按什么理由说完了的」永远有答案
CREATE TABLE purchase_order_payment_closures (
    id              BIGSERIAL     PRIMARY KEY,
    tenant_id       BIGINT        NOT NULL DEFAULT 1,
    po_id           BIGINT        NOT NULL REFERENCES purchase_orders(id),
    -- 确认完成那一刻的未付数（订单金额 − 已付净额）快照。之后这个数还会
    -- 动，认的是**当时**那个数。可以是负的：多付了、不追回、就这么了结，
    -- 也是一种了结。所以这里没有任何符号上的 CHECK。
    open_amount     NUMERIC(18,2) NOT NULL,
    -- 为什么算完了。SETTLED（正常付清）是最常见的一档——既然完成与否完全
    -- 由人确认，「付齐了，正常结案」就必须有位置放。客户侧 00021 补过同一课。
    category        VARCHAR(16)   NOT NULL
        CHECK (category IN ('SETTLED', 'LOSS', 'ROUNDING', 'CANCELLED', 'OTHER')),
    note            TEXT          NOT NULL DEFAULT '',
    closed_by_id    BIGINT        NOT NULL DEFAULT 0,
    closed_by_name  VARCHAR(100)  NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),

    revoked_at      TIMESTAMPTZ,
    revoked_by_id   BIGINT        NOT NULL DEFAULT 0,
    revoked_by_name VARCHAR(100)  NOT NULL DEFAULT '',
    revoke_reason   TEXT          NOT NULL DEFAULT ''
);

-- 一张采购单同时只能有一条活着的确认。两个人同时点「确认完成」，只有一个
-- 写得进去，另一个撞 23505——app 层把它翻成人话，而不是靠先读一次再写。
CREATE UNIQUE INDEX po_payment_closures_live
    ON purchase_order_payment_closures (tenant_id, po_id)
    WHERE revoked_at IS NULL;

-- 待核销页要按「有没有活着的确认」左连接过滤，走这条。
CREATE INDEX po_payment_closures_po_idx
    ON purchase_order_payment_closures (tenant_id, po_id);

-- **历史数据不回填**：一条 closure 都不种，所有既有采购订单一律落在
-- 「待核销」页上，由人一条条确认。这是需求明确要的，不是漏了。

-- +goose Down
DROP TABLE IF EXISTS purchase_order_payment_closures;
ALTER TABLE payment_allocations DROP COLUMN IF EXISTS note;
ALTER TABLE payment_allocations DROP COLUMN IF EXISTS paid_at;
-- Down 不是 Up 的镜像：payment_id 变回 NOT NULL 之前必须先清掉手填行，
-- 否则 ALTER 直接失败。手填行结构上没有付款单可挂，回滚只能删——
-- **回滚的代价就是丢掉所有手填的核销记录**，写在这里让人下决定前看得见。
-- TestUpDownUp 在空库上跑，这一句删的是零行。
DELETE FROM payment_allocations WHERE payment_id IS NULL;
ALTER TABLE payment_allocations ALTER COLUMN payment_id SET NOT NULL;
