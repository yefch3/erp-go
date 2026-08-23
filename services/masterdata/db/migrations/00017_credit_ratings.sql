-- +goose Up

-- 信用评级（E3 第一期）：客户和供应商各评一个 A/B/C/D。
--
-- 业务的原话是「供应商和客户需要信用度评级，分为 ABCD」。
--
-- **不做成一个下拉框就完事。** 一个能填的字段会变成填过一次再没人动过的
-- 死数据——半年后看见「B」，没人知道那是上周判的还是三年前判的、当时看
-- 的是什么。所以评级本身只是结果，真正立得住的是这张表：每改一次留一行，
-- 依据必填。
--
-- 一张表管两边。客户评的是「会不会按时付钱」，供应商评的是交期准不准、
-- 供货足不足量、质量合格率——问的问题不同，但「谁在什么时候依据什么评了
-- 什么」这个形状是同一个，拆成两张表只会让历史查询写两遍。
CREATE TABLE credit_ratings (
    id           BIGSERIAL    PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL DEFAULT 1,
    party_type   VARCHAR(16)  NOT NULL CHECK (party_type IN ('CUSTOMER', 'SUPPLIER')),
    party_id     BIGINT       NOT NULL,
    grade        VARCHAR(1)   NOT NULL CHECK (grade IN ('A', 'B', 'C', 'D')),
    -- 上一次是什么。空串表示首次评级。存下来而不是查上一行，是为了让
    -- 「从 B 降到 C」这件事在一行里读得完——降级是要有人过问的事。
    previous_grade VARCHAR(1) NOT NULL DEFAULT ''
                              CHECK (previous_grade IN ('', 'A', 'B', 'C', 'D')),
    -- 依据。非空是这张表存在的理由：说不出为什么的评级，等于没评。
    basis        TEXT         NOT NULL CHECK (btrim(basis) <> ''),
    -- 评级当时系统里的那几个数（交期准时率、供货足量率、质检合格率、
    -- 逾期天数……）。现在大多是空的——合同和采购单还没跑起来，算出来只会
    -- 是 0。留着这个位置，是为了等数据攒起来之后，历史里能看出「当时依据
    -- 的数字长这样」，而不是只剩一句话。
    evidence     JSONB        NOT NULL DEFAULT '{}',
    rated_by     BIGINT       NOT NULL,
    rated_by_name VARCHAR(100) NOT NULL DEFAULT '',
    rated_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX credit_ratings_party_idx
    ON credit_ratings (tenant_id, party_type, party_id, rated_at DESC);

-- 类型是 VARCHAR(1) 而不是 CHAR(1)，这一个字的差别是踩出来的：CHAR 会把
-- 空串补成一个空格，于是「没评过」在读出来时变成一个看不见的空格——判空
-- 失败，页面上按钮写着「重新评级」，而那家客户一次都没评过。
--
-- 当前评级冗余在主数据行上：列表要按评级筛、要排序，每行去翻历史表取最新
-- 一条是 N+1。历史表仍是事实来源，这两列是它的投影。
--
-- graded_at 单独一列，为的是回答「多久没评了」——那是一个不用连表的
-- 排序条件，也是这套东西不腐烂的唯一保障：超过约定时间没人动过，列表上
-- 自己会显出来。
ALTER TABLE customers
    ADD COLUMN credit_grade     VARCHAR(1)  NOT NULL DEFAULT ''
                                CHECK (credit_grade IN ('', 'A', 'B', 'C', 'D')),
    ADD COLUMN credit_graded_at TIMESTAMPTZ;

ALTER TABLE suppliers
    ADD COLUMN credit_grade     VARCHAR(1)  NOT NULL DEFAULT ''
                                CHECK (credit_grade IN ('', 'A', 'B', 'C', 'D')),
    ADD COLUMN credit_graded_at TIMESTAMPTZ;

CREATE INDEX customers_credit_grade_idx
    ON customers (tenant_id, credit_grade, credit_graded_at)
    WHERE credit_grade <> '';
CREATE INDEX suppliers_credit_grade_idx
    ON suppliers (tenant_id, credit_grade, credit_graded_at)
    WHERE credit_grade <> '';

-- +goose Down

DROP INDEX IF EXISTS suppliers_credit_grade_idx;
DROP INDEX IF EXISTS customers_credit_grade_idx;
ALTER TABLE suppliers DROP COLUMN credit_graded_at, DROP COLUMN credit_grade;
ALTER TABLE customers DROP COLUMN credit_graded_at, DROP COLUMN credit_grade;
DROP TABLE credit_ratings;
