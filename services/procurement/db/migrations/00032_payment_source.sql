-- +goose Up

-- 付款单从哪来。
--
-- 阶段 4 之后付款单有两个来源：手工登记的（MANUAL），和在「付款对账」里
-- 直接核销一条银行流水时系统自动建的影子单（BANK）。影子单和手工单共用
-- 同一个号段、同一套核销守门——这一列只负责让财务在列表里认出「这张单
-- 是系统替你写的」，防止被当成重复录入删掉或再录一遍。
ALTER TABLE supplier_payments
    ADD COLUMN source VARCHAR(16) NOT NULL DEFAULT 'MANUAL'
        CHECK (source IN ('MANUAL', 'BANK'));

-- +goose Down
ALTER TABLE supplier_payments DROP COLUMN source;
