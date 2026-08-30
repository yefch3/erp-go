-- +goose Up

-- 应付到期日改成员工手填。
--
-- 上一版（00040）的口径是「下单那天 + 供应商配的账期」，账期从主数据快照
-- 到单上。业务把这条口径推翻了：**每一份采购单的应付日期都是这份单自己
-- 的一部分，建单时由人填**。同一家供应商，这批货谈 30 天、下批谈预付，
-- 都是常事——把它挂在供应商身上，等于说这家公司只有一种付款条件。
--
-- 于是 payment_days 这一列失去了全部意义：
--
--   * 它从来没有被任何 SELECT 读出去过，只在 INSERT / UPDATE 里出现；
--   * 它唯一的消费者是 SetPurchaseOrderOrdered 里那句
--     `payable_due_date = CASE WHEN payment_days > 0 THEN ... ELSE NULL END`
--     ——那句本身正在这一批里删掉。
--
-- 留着它比删掉危险：那句 CASE 是**无条件赋值**，而列一旦恒为 0 就会走
-- ELSE 分支，把员工填好的日子在审批通过那一刻悄悄抹成空。抹的动作发生在
-- Kafka 审批消费里，没有人在场、不报错、页面只显示「未配账期」。
-- **这一版不删列**，理由同 masterdata 00020：先迁库后换容器，回滚回去的
-- 旧代码还会往这一列写值（CreatePurchaseOrder 的 INSERT 里有它）。这一批
-- 已经把读写都摘干净，列留着当块无人问津的石头，下一版再删。
--
-- 真正要紧的那一半这一版就得做完：SetPurchaseOrderOrdered 里那句
-- `payable_due_date = CASE WHEN payment_days > 0 ... ELSE NULL END` 必须
-- 现在就删掉。留着它，列恒为 0 就会走 ELSE 分支，把员工填好的日子在审批
-- 通过那一刻抹成空——而那条 UPDATE 跑在 Kafka 审批消费里，没有人在场、
-- 不报错、页面只会显示「未填到期日」。

-- 改到期日的留痕。
--
-- 到期日既然是单据的一部分，谈判改了就该能改；而「这张单什么时候该付钱」
-- 直接决定它算不算逾期，所以改动必须留下谁、什么时候、从哪天改到哪天、
-- 为什么。没有这张表，「谁把这单的逾期改没了」事后查不出来。
--
-- 只记不拦：这里不做任何权限判断，围栏在服务层。
CREATE TABLE purchase_order_due_changes (
    id            BIGSERIAL    PRIMARY KEY,
    tenant_id     BIGINT       NOT NULL DEFAULT 1,
    po_id         BIGINT       NOT NULL REFERENCES purchase_orders(id),
    -- 改之前是什么。可空——「本来就没填」也是一种之前的状态，而且是最
    -- 常见的那种（存量单全是空的）。
    old_due_date  DATE,
    -- 改之后是什么。同样可空：把一个填错的日子清空，是合法的一次改动。
    new_due_date  DATE,
    reason        TEXT         NOT NULL,
    changed_by_id   BIGINT     NOT NULL DEFAULT 0,
    changed_by_name VARCHAR(100) NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX purchase_order_due_changes_po_idx
    ON purchase_order_due_changes (tenant_id, po_id, id DESC);

-- +goose Down

DROP TABLE IF EXISTS purchase_order_due_changes;

