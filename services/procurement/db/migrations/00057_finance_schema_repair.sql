-- +goose Up

-- 部分长期运行的数据库先记录了较高的 goose 版本，随后合入的 00037/00046
-- 因编号较小没有被回头执行。全部语句保持幂等，只补当前财务页面必需的结构。
ALTER TABLE payment_allocations ALTER COLUMN payment_id DROP NOT NULL;
ALTER TABLE payment_allocations ADD COLUMN IF NOT EXISTS paid_at DATE;
ALTER TABLE payment_allocations ADD COLUMN IF NOT EXISTS note TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS purchase_order_payment_closures (
    id              BIGSERIAL     PRIMARY KEY,
    tenant_id       BIGINT        NOT NULL DEFAULT 1,
    po_id           BIGINT        NOT NULL REFERENCES purchase_orders(id),
    open_amount     NUMERIC(18,2) NOT NULL,
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
CREATE UNIQUE INDEX IF NOT EXISTS po_payment_closures_live
    ON purchase_order_payment_closures (tenant_id, po_id)
    WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS po_payment_closures_po_idx
    ON purchase_order_payment_closures (tenant_id, po_id);

ALTER TABLE bank_transactions ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE bank_transactions ADD COLUMN IF NOT EXISTS deleted_by_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE bank_transactions ADD COLUMN IF NOT EXISTS deleted_by_name VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE bank_transactions ADD COLUMN IF NOT EXISTS delete_reason TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS bank_transactions_live_list_idx
    ON bank_transactions (tenant_id, txn_date DESC, id DESC)
    WHERE deleted_at IS NULL;

-- +goose Down
-- 修复迁移不删除结构：这些字段和表也属于 00037/00046 的正式结构。
SELECT 1;
