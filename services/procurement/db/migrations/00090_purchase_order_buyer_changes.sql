-- +goose Up

-- 采购单换采购员的留痕（#643）。
--
-- 采购单按采购员决定谁看得见。历史补录的单以前一律记在录入人名下、换不了，
-- 而补录常常是公司派一个人统一录——真正负责的采购「只看得到自己的单」，一张
-- 都找不到。现在历史补录的单可以换采购员（app/order_buyer.go）。
--
-- 换采购员等于换了「谁看得见这张单、谁对它负责」，所以每一次都要留下：谁、
-- 什么时候、从谁换到谁、为什么。没有这张表，「我的单怎么不见了」事后查不出来。
--
-- 名字和编号一起存：人会改名、会离职，记录要读得懂当时是谁。
-- 只记不拦：这里不做任何权限判断，围栏在服务层。
CREATE TABLE purchase_order_buyer_changes (
    id              BIGSERIAL    PRIMARY KEY,
    tenant_id       BIGINT       NOT NULL,
    po_id           BIGINT       NOT NULL REFERENCES purchase_orders(id),
    old_buyer_id    BIGINT       NOT NULL,
    old_buyer_name  VARCHAR(100) NOT NULL DEFAULT '',
    new_buyer_id    BIGINT       NOT NULL,
    new_buyer_name  VARCHAR(100) NOT NULL DEFAULT '',
    -- 选填：统一录入后按人分单是例行操作，逼着每张都写一句原因只会收获一串「分配」。
    reason          TEXT         NOT NULL DEFAULT '',
    changed_by_id   BIGINT       NOT NULL,
    changed_by_name VARCHAR(100) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX purchase_order_buyer_changes_po_idx
    ON purchase_order_buyer_changes (tenant_id, po_id, id DESC);

-- +goose Down

DROP TABLE IF EXISTS purchase_order_buyer_changes;
