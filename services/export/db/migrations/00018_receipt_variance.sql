-- +goose Up

-- 差额：一笔钱和它该对上的数对不上时，把「差在哪、差多少、谁认的」记下来。
--
-- 现实里全款和合同金额经常对不齐——损耗扣款、尾差、多付。原来系统对此的
-- 回答是「核不满就永远挂在待处理」：一笔已经处理完的钱天天躺在待办里，
-- 几周之后整个队列就没人信了。这次给两种对不齐各自一个去处：
--
--   合同侧（到账 < 合同额）：核销行上的 fee_amount 本来就是干这个的——
--   「不从银行那一行出、只把合同补满」。原来它只有手续费一种含义，现在
--   加一列类别，损耗、尾差都能如实记，不用一律谎报成手续费。
--
--   流水侧（到账 > 能核的）：新表 receipt_line_settlements，见下。

-- 类别先落库、后启用：CHECK 允许四种，代码这一版全部写 BANK_FEE 之外的
-- 值也校验。历史行统一回填 BANK_FEE——它们当年就是按手续费的意思填的。
ALTER TABLE receipt_allocations
    ADD COLUMN fee_category VARCHAR(16) NOT NULL DEFAULT 'BANK_FEE'
    CHECK (fee_category IN ('BANK_FEE', 'LOSS', 'ROUNDING', 'OTHER'));

-- 流水行的「认差结清」：核到没得核了还剩一截，员工看过、说了这截是什么，
-- 这一行才算完。
--
-- 是一张表不是一列，因为「认」是一个有主语的动作：谁、什么时候、按什么
-- 类别、留了什么话。撤销不删行——revoked_at 一填，历史还在，查「这 200
-- 当初是谁认掉的」永远有答案。这和核销记录的 append-only 是同一个纪律。
--
-- transaction_id 指向采购库的 bank_transactions（F2 之后账本只有一本），
-- 跨库没有外键可立，和 receipt_allocations 同一处境。
CREATE TABLE receipt_line_settlements (
    id              BIGSERIAL     PRIMARY KEY,
    tenant_id       BIGINT        NOT NULL,
    transaction_id  BIGINT        NOT NULL,
    -- 认下的差额。就是「结清那一刻还没核出去的余额」，存下来而不是每次
    -- 现算，因为之后核销记录还会变（冲销），而认的是**当时**那个数。
    amount          NUMERIC(18,2) NOT NULL CHECK (amount > 0),
    -- 这截钱是什么。LOSS 损耗扣款 / ROUNDING 尾差 / OVERPAY 客户多付 /
    -- OTHER 其他。没有 BANK_FEE：手续费补的是合同，走上面那一列。
    category        VARCHAR(16)   NOT NULL
        CHECK (category IN ('LOSS', 'ROUNDING', 'OVERPAY', 'OTHER')),
    note            TEXT          NOT NULL DEFAULT '',
    settled_by_id   BIGINT        NOT NULL DEFAULT 0,
    settled_by_name VARCHAR(100)  NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    revoked_at      TIMESTAMPTZ,
    revoked_by_id   BIGINT        NOT NULL DEFAULT 0,
    revoked_by_name VARCHAR(100)  NOT NULL DEFAULT '',
    revoke_reason   TEXT          NOT NULL DEFAULT ''
);

-- 一行流水同时只能有一个活着的结清。撤销过的不占位——认错了、撤了、
-- 再认一次，是正常路径。
CREATE UNIQUE INDEX receipt_line_settlements_live
    ON receipt_line_settlements (tenant_id, transaction_id)
    WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS receipt_line_settlements;
ALTER TABLE receipt_allocations DROP COLUMN IF EXISTS fee_category;
