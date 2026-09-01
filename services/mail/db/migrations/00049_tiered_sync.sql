-- +goose Up

-- 上一次向服务器问过这个信箱的「轻状态」是什么时候。
--
-- 轻状态 = IMAP 的 STATUS：一条命令、不打开信箱、不拉任何正文，回的是
-- UIDNEXT 和 UNSEEN 两个数。它回答的只有一个问题——**这个箱里有没有我们
-- 还没取过的信**。
--
-- **为什么需要这一列：全量同步在多信箱之后摊不开了。**
--
-- 300 人的公司，一人两个箱就是 600 个箱。8 个 worker、两分钟一轮，平均每个
-- 箱只有 1.6 秒——跨太平洋一次握手就超了。轮次拖长的样子不是报错，是「信
-- 晚到十分钟」，而且是**所有人**一起晚。
--
-- 分档之后：有人在看的箱（last_read_at 新鲜，见 00048）照旧全量同步，那部分
-- 大约 50 个，每个箱有 19 秒；剩下的只问 STATUS，十分钟摊一遍。
--
-- **分档只改延迟，不改对错。** 一个没人看的箱来了新信，STATUS 会看见
-- UIDNEXT 超过我们存的 last_uid，那一轮就把它提上来全量同步。所以最坏情况
-- 是「晚十分钟」，不是「收不到」——这一点是这个设计能成立的全部依据。
--
-- 空表示从没问过，那时立刻问。刚部署时所有行都是空的，于是第一轮全问一遍，
-- 之后自然错开。
ALTER TABLE mail_accounts ADD COLUMN status_checked_at TIMESTAMPTZ;

-- 轮询每轮都要挑「该问 STATUS 的那些」。没有索引的话这是一次全表扫，而它
-- 每两分钟跑一次、每家公司一次。
--
-- 部分索引：只有在同步的箱才会被挑中，停用的那些不必进树。
CREATE INDEX mail_accounts_status_due_idx
    ON mail_accounts (tenant_id, status_checked_at NULLS FIRST)
    WHERE is_active;

-- +goose Down

DROP INDEX IF EXISTS mail_accounts_status_due_idx;
ALTER TABLE mail_accounts DROP COLUMN status_checked_at;
