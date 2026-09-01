-- +goose Up

-- 改流水的留痕。
--
-- 登记流水现在可以改了——打错日期、金额多个零、对方名字写错，直接改，不用
-- 走「删掉重新登记」（而那条路还会撞上 bank_ref 的唯一键）。
--
-- **但改的是账。** 一笔钱的金额、日期、流水号，改过一次而没人知道，是这种表
-- 最难查的问题：报表对不上，谁也说不清是当初录错了还是后来被人改了。所以
-- 每改一次，谁、什么时候、哪个字段、从什么改成什么、为什么，全留下。
--
-- 只留「会影响账」的那几项（日期、方向、金额、币种、流水号、对方名称）。
-- 归属、备注、附件这些「我们自己说的」有各自的入口，改它们不动账，不留。
--
-- 一次编辑可能改好几个字段，那就是好几行——一个字段一行，而不是把整行
-- 存成 JSON 快照。查的时候问的从来是「这个金额被谁改过」，不是「2 月 3 号
-- 这一行长什么样」；一个字段一行，那个问题是一次 WHERE，快照则要把每一版
-- 都解开来比。
--
-- 只记不拦：这里没有任何权限判断，围栏在服务层。
CREATE TABLE bank_transaction_changes (
    id           BIGSERIAL   PRIMARY KEY,
    tenant_id    BIGINT      NOT NULL DEFAULT 1,
    txn_id       BIGINT      NOT NULL REFERENCES bank_transactions(id),
    -- 改的是哪一项。存字段名（txn_date / amount / bank_ref ...），不是中文
    -- 标签——标签会跟着界面改，字段名不会。
    field        VARCHAR(40) NOT NULL,
    -- 改之前和改之后，一律按文本存。
    --
    -- 金额、日期、方向、流水号在这里是同一种东西：「当时写的是什么」。
    -- 按各自的类型分列存，就要六组列而且大部分永远是空的；而这张表没有
    -- 任何人会拿去做算术——它只被人读。
    old_value    TEXT        NOT NULL DEFAULT '',
    new_value    TEXT        NOT NULL DEFAULT '',
    -- 为什么改。**必填**，和删除同一个规矩：一条没有理由的改动记录，
    -- 事后翻到它还是不知道当时发生了什么。
    reason       TEXT        NOT NULL,
    changed_by_id   BIGINT       NOT NULL DEFAULT 0,
    changed_by_name VARCHAR(100) NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 查法只有一种：看这一行流水被改过什么，最近的排前面。
CREATE INDEX bank_transaction_changes_txn_idx
    ON bank_transaction_changes (tenant_id, txn_id, id DESC);

-- +goose Down

DROP TABLE IF EXISTS bank_transaction_changes;
