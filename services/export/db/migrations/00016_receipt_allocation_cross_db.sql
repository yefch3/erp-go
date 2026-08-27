-- +goose Up

-- F2 第二步（上半）：核销记录要能指向**另一个库**里的那一行。
--
-- 银行流水以后只有一份，在采购的库里（见 procurement/00028）。核销记录留在
-- 这本账里不动——它是出口业务的判断，属主是出口——但它指的那一行搬到了
-- 别的库，而**跨库不能有外键**。所以这条约束必须去掉。
--
-- 去掉之后 transaction_id 还是同一个意思：「这笔核销是从哪一行银行流水里扣
-- 的」。变的只是谁来把关。原来是数据库替我们挡住「指向一行不存在的流水」，
-- 现在这个检查挪到核销那一步：出口服务在写核销之前会先去采购把那一行取出
-- 来（取不到就拒绝，取到了才知道金额和币种能不能核）。换句话说，把关的人
-- 从数据库换成了那段必须存在的代码——那段代码在下一版（2b）写。
--
-- 为什么现在就去掉、而不是等到 2b：去掉一条约束**从来不会让原本能跑的代码
-- 跑不了**。约束是拦人的，不是让事情成立的。这一版即使部署失败被回滚，旧的
-- 出口服务回来照样能插核销记录，只是数据库不再多检查一道。所以它可以和加列
-- 一起先发，把下一版留成纯代码改动。
--
-- **这一版不删 bank_transactions 这张表。** 它是空的（生产上 0 行，
-- 2026-08-26 用 scripts/f2-bank-merge-report.sh 查过），留着不花任何成本，
-- 而删掉就要担一个回滚救不回来的风险窗口：迁移先跑、容器后换，健康检查一旦
-- 因为**任何**原因没过，回滚回来的旧出口服务会去查一张已经不存在的表。
-- 等 2b 上线跑稳之后，单独发一次清理，那时候的理由才是实打实的。

ALTER TABLE receipt_allocations
    DROP CONSTRAINT receipt_allocations_transaction_id_fkey;

-- +goose Down
ALTER TABLE receipt_allocations
    ADD CONSTRAINT receipt_allocations_transaction_id_fkey
        FOREIGN KEY (transaction_id) REFERENCES bank_transactions(id);
