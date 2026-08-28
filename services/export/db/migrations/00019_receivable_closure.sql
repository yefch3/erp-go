-- +goose Up

-- 收款结清：一张合同的钱「不用再催了」，由人说出来、记下来。
--
-- 原来「收完没有」完全是算出来的：sum(核销) < 合同额就催。这在没有退款的
-- 世界里刚好——收满自动消失。但收付队列马上要放退款进来（负的核销行），
-- 一笔退款会让已收变小、未收重新变正，于是一张业务上已经了结的合同
-- **自己爬回催收清单**，而且提醒的去重键里带着「逾期第几轮」，每 7 天
-- 稳稳再发一封，永不停止。
--
-- 结清是给「算式说还欠、人说不欠了」的情况一个出口：损耗认了、尾差不追、
-- 合同取消退了款。它只关催收的口，**不关钱的门**——结清的合同照样能核销
-- （钱真的又来了就再撤销结清），选合同的下拉里搜关键词也照样搜得到。
--
-- 和 receipt_line_settlements 同一套纪律：撤销不删行，revoked_at 一填
-- 历史还在，「这张合同当初是谁、按什么理由停催的」永远有答案。
CREATE TABLE contract_receivable_closures (
    id             BIGSERIAL     PRIMARY KEY,
    tenant_id      BIGINT        NOT NULL,
    contract_id    BIGINT        NOT NULL REFERENCES contracts(id),
    -- 结清那一刻的未收数，快照。之后核销/退款还会动，认的是**当时**那个数。
    -- 可以是负的：多收着钱结清（客户多付了不退）也是一种了结。
    open_amount    NUMERIC(18,2) NOT NULL,
    -- 为什么不催了。LOSS 损耗认了 / ROUNDING 尾差 / CANCELLED 合同取消
    -- 或终止 / OTHER 其他。
    category       VARCHAR(16)   NOT NULL
        CHECK (category IN ('LOSS', 'ROUNDING', 'CANCELLED', 'OTHER')),
    note           TEXT          NOT NULL DEFAULT '',
    closed_by_id   BIGINT        NOT NULL DEFAULT 0,
    closed_by_name VARCHAR(100)  NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
    revoked_at     TIMESTAMPTZ,
    revoked_by_id  BIGINT        NOT NULL DEFAULT 0,
    revoked_by_name VARCHAR(100) NOT NULL DEFAULT '',
    revoke_reason  TEXT          NOT NULL DEFAULT ''
);

-- 一张合同同时只能有一个活着的结清。
CREATE UNIQUE INDEX contract_receivable_closures_live
    ON contract_receivable_closures (tenant_id, contract_id)
    WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS contract_receivable_closures;
