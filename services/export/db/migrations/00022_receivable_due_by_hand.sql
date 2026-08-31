-- +goose Up

-- 应收到期日改成员工手填。
--
-- 列本身（contracts.receivable_due_date，00013 建的）不动，改的是**谁来写
-- 它**：从「合同生效那一刻按客户主数据的账期算」，改成「建合同的人自己
-- 填」。同一个客户，这一单谈 60 天、下一单要求预付，都是常事。
--
-- 为什么留在 contracts 主表而不是搬去 contract_versions：版本表上有
-- contract_versions_freeze 触发器（00003），审批通过之后任何一列都改不动，
-- 应用层绕不过。到期日既然要能事后改（改错了、谈判变了），放版本表意味着
-- 改一个打错的日子要走「变更 → 重新审批 → 重新签署」，而重新签署会再发
-- 一次 ContractEffective，下游采购、物流、库存全部再收一遍。
CREATE TABLE contract_due_changes (
    id              BIGSERIAL    PRIMARY KEY,
    tenant_id       BIGINT       NOT NULL DEFAULT 1,
    contract_id     BIGINT       NOT NULL REFERENCES contracts(id),
    -- 两头都可空：「本来没填」和「改回没填」都是合法状态。
    old_due_date    DATE,
    new_due_date    DATE,
    reason          TEXT         NOT NULL,
    changed_by_id   BIGINT       NOT NULL DEFAULT 0,
    changed_by_name VARCHAR(100) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX contract_due_changes_contract_idx
    ON contract_due_changes (tenant_id, contract_id, id DESC);

-- +goose Down

DROP TABLE IF EXISTS contract_due_changes;
