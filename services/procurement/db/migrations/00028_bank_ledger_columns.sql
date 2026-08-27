-- +goose Up

-- F2 第二步（上半）：把这张表补成**唯一**的银行流水账本。
--
-- 今天 `bank_transactions` 在两个库里各有一份：这里一份（CSV 导入，对供应商
-- 付款单），出口库里还有一份（手工登记，对出口合同）。第一步已经给这里加了
-- 「归属」，让一行流水能说清自己是谁那条线上的钱；这一步把出口那份**独有的
-- 信息**补进来，好让收款对账下一版能直接读这里。
--
-- 这一版只加东西，不删、不改、不搬。出口那张表原样留着、照旧被收款对账读，
-- 界面上一个字都不会变——**这是故意的**。部署顺序是「先迁库、后换容器」，
-- 回滚只换容器不换库，所以「加列」和「切读」必须分两次发布：这一版即使
-- 因为任何原因回滚，旧代码看到的还是它熟悉的库。
--
-- 生产上这张表和出口那张都是 0 行（2026-08-26 用 scripts/f2-bank-merge-report.sh
-- 查过），所以没有数据要迁、没有重号要人工核对。

-- 我们自己的账户清单。它跟着**账本**走而不是跟着服务走：账本在这个库里，
-- 账户清单也得在这个库里，否则下面那个 account_id 就是一个指不到任何地方
-- 的数字。
--
-- 它存在的理由和出口当初建它时一模一样，一个字没变：两个自己账户之间转钱
-- （外币户结汇进人民币户）在流水上是一笔大额进账，**长得和客户打款一模
-- 一样**。没有这份清单，这笔钱会被核销到某张合同上，而账错了一个月都不会
-- 有人发现。
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
CREATE INDEX bank_accounts_list_idx ON bank_accounts (tenant_id, status, id);

ALTER TABLE bank_transactions
    -- 钱进了/出了我们哪个账户。**0 = 没填**，不是「第 0 号账户」——
    -- 已经导进来的那些 CSV 行没有这个信息，硬给它们填一个会是假的，而一个
    -- 编出来的账户号会让上面那条「内部划转认成客户打款」的防线失效。
    --
    -- 故意不加外键：这一列既要能是 0（未知），又要在填了的时候指向
    -- bank_accounts。外键管不了「0 表示空」这件事，检查放在写入那一步。
    ADD COLUMN account_id BIGINT NOT NULL DEFAULT 0,

    -- 对方账号。同名不同号的公司在银行对账单里天天出现，只看名字对不准。
    ADD COLUMN counterparty_account VARCHAR(100) NOT NULL DEFAULT '',

    -- 汇款附言（MT103 第 70 域，或国内汇款的「用途」栏）。**扫合同号靠它。**
    --
    -- 和已有的 remark 不是一回事，所以不能合成一列：remark 是 CSV 里那一列
    -- 备注，是银行导出文件的产物；remittance_info 是**付款人自己写进这笔汇款
    -- 里的话**，客户把合同号写在哪里，就写在这里。合成一列，下一版的合同号
    -- 建议就没得扫了。
    ADD COLUMN remittance_info TEXT NOT NULL DEFAULT '',

    -- 这一行是怎么进来的。已有行全部来自 CSV 导入（在这一版之前，
    -- ImportBankStatement 是唯一往这张表写的地方），所以默认值就是事实，
    -- 不是猜的。
    ADD COLUMN source VARCHAR(16) NOT NULL DEFAULT 'CSV'
        CHECK (source IN ('MANUAL','CSV','API','PSP')),

    -- 我们自己发出去的号（收款链接一类）。和客户手打的附言不同，这个是可信
    -- 的，所以**只有它有资格驱动自动核销**——附言里出现的合同号永远只是给
    -- 人看的建议。
    ADD COLUMN trusted_ref VARCHAR(100) NOT NULL DEFAULT '',

    -- 人写的备注。和银行给的 remark 分开：一个是银行说的，一个是我们说的，
    -- 混在一起以后就分不清哪句话是谁写的了。
    ADD COLUMN note TEXT NOT NULL DEFAULT '';

-- 收款对账下一版要按「归属 = 客户」筛这张表，走的就是这条索引。
-- （第一步建的 bank_transactions_ownership_idx 是同样的形状，这里不重复建。）

-- +goose Down
ALTER TABLE bank_transactions
    DROP COLUMN note,
    DROP COLUMN trusted_ref,
    DROP COLUMN source,
    DROP COLUMN remittance_info,
    DROP COLUMN counterparty_account,
    DROP COLUMN account_id;
DROP TABLE bank_accounts;
