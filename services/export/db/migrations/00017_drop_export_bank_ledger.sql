-- +goose Up

-- migration-safety: DROP TABLE bank_transactions / bank_accounts ——
--   自 2b（PR #251）起，出口服务已无任何代码引用这两张表：那一版删掉了读写
--   它们的全部 7 个查询，并把银行流水改成通过 gRPC 问采购服务。#251 已于
--   2026-08-26 合并并**部署成功**，生产 HEAD = c791a21。
--   同日在生产上查过：两张表均 0 行，receipt_allocations 也是 0 条，
--   且 receipt_allocations 上只剩 contract_id / reversal_of 两条外键
--   （指向 bank_transactions 的那条已在 2a 去掉）。
--   —— 方晨，2026-08-26

-- F2 第二步的收尾：把出口这边的银行流水账清掉。
--
-- 这两张表在 F2 之前是出口自己的一本银行流水账，收款对账页用它。并表之后
-- 只剩采购那一本（`erp_procurement.bank_transactions` 与同库的
-- `bank_accounts`），这两张就成了没人读的空壳。
--
-- **为什么拖到现在才删，而不是跟 2a/2b 一起。**
--
-- `deploy/deploy.sh` 的顺序是「先跑迁移改库、再换容器」，健康检查失败时
-- `git checkout $PREV` 换回旧容器——**回滚只换容器，一行都不碰数据库**。
-- 所以「删表」和「让新代码不再读它」放在同一次发布里，等于把一次本来能自愈
-- 的部署失败变成一次要人手动进库修的事故：旧版本的出口服务回来，去查一张
-- 已经不存在的表，收款对账直接报错，而回滚救不回来。而那次失败的原因**可能
-- 跟这个改动毫无关系**——同批别人的代码、30 秒不够某个服务起来、网络抖一下。
--
-- 分成两次发布之后，这次删的是一张**已经上线验证过没人读**的表。上面那段
-- 声明里的每一句都是已经发生过的事实，不是动手之前找的理由。
--
-- 顺带说清楚一件事：`receipt_allocations` **不删**。核销记录是出口的判断，
-- 属主是出口，它留在这个库里；只是 `transaction_id` 现在指的是采购库里那
-- 一行的 id（跨库，所以是普通 bigint，2a 已经去掉了那条外键）。

DROP TABLE bank_transactions;
DROP TABLE bank_accounts;

-- +goose Down

-- 回滚只重建结构，不还原数据——数据在被删的时候就是 0 行，没有什么可还原
-- 的。这两段 DDL 抄自 00010_receipts.sql，一个字没改。
CREATE TABLE bank_accounts (
    id           BIGSERIAL    PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL DEFAULT 1,
    account_no   VARCHAR(64)  NOT NULL,
    account_name VARCHAR(200) NOT NULL,
    bank_name    VARCHAR(200) NOT NULL DEFAULT '',
    currency     VARCHAR(8)   NOT NULL DEFAULT 'USD',
    status       VARCHAR(16)  NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, account_no)
);

CREATE TABLE bank_transactions (
    id              BIGSERIAL     PRIMARY KEY,
    tenant_id       BIGINT        NOT NULL DEFAULT 1,
    account_id      BIGINT        NOT NULL REFERENCES bank_accounts(id),
    bank_ref        VARCHAR(100)  NOT NULL,
    direction       VARCHAR(8)    NOT NULL CHECK (direction IN ('CREDIT','DEBIT')),
    amount          NUMERIC(18,2) NOT NULL CHECK (amount > 0),
    currency        VARCHAR(8)    NOT NULL,
    value_date      DATE          NOT NULL,
    counterparty    VARCHAR(200)  NOT NULL DEFAULT '',
    counterparty_account VARCHAR(100) NOT NULL DEFAULT '',
    remittance_info TEXT          NOT NULL DEFAULT '',
    source          VARCHAR(16)   NOT NULL DEFAULT 'MANUAL'
        CHECK (source IN ('MANUAL','CSV','API','PSP')),
    trusted_ref     VARCHAR(100)  NOT NULL DEFAULT '',
    disposition     VARCHAR(20)   NOT NULL DEFAULT 'UNPROCESSED'
        CHECK (disposition IN ('UNPROCESSED','ALLOCATED','IRRELEVANT')),
    irrelevant_type VARCHAR(32)   NOT NULL DEFAULT ''
        CHECK (irrelevant_type IN ('','TAX_REFUND','INTEREST','INTERNAL','SUPPLIER_REFUND','DEPOSIT_RETURN','OTHER')),
    note            TEXT          NOT NULL DEFAULT '',
    recorded_by     BIGINT        NOT NULL DEFAULT 0,
    recorded_by_name VARCHAR(100) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, account_id, bank_ref)
);
CREATE INDEX bank_tx_queue_idx ON bank_transactions (tenant_id, disposition, value_date DESC);
CREATE INDEX bank_tx_party_idx ON bank_transactions (tenant_id, counterparty);
