-- +goose Up

-- 一个人可以绑多个信箱。
--
-- migration-safety: DROP CONSTRAINT mail_accounts_tenant_id_employee_id_key ——
--   放宽约束，不是收紧：删掉之后旧版本代码写得进去的东西一行不少，读得出来
--   的也一行不少。回滚容器不会遇到读不懂的库。真正要小心的是反过来那一半，
--   见下面 UpsertMailAccountShell 那段。2026-08-30 确认。
--
-- 原来的注释写的是：
--
--     One mailbox per person: two rows would make "which account sent this"
--     ambiguous at exactly the moment somebody is trying to trace a message.
--
-- 那个担心是对的，而且今天仍然对——出站表 email_messages 只有 sender_id，
-- 没有 account_id，所以"这封信从哪个信箱发出去的"确实答不上来。**这一版
-- 还没有任何界面能加第二个信箱**，第三期给出站队列加上 account_id 之后
-- 才会真的出现多行。先把地基放开，是因为写入路径必须和约束同批改（见下）。
--
-- UNIQUE (tenant_id, email) 保留：一个信箱只能属于一个人。两个人绑同一个
-- 地址，谁都说不清那封信该算谁的，而且两边的授权码会互相覆盖。
ALTER TABLE mail_accounts DROP CONSTRAINT mail_accounts_tenant_id_employee_id_key;

-- 默认信箱：写信时预选哪一个。
--
-- 和登录地址无关——业务上已经定了「ERP 账号是 263 的人，邮箱这边可以只绑
-- Gmail」。所以这是一面纯粹的偏好旗子，不是身份。
ALTER TABLE mail_accounts ADD COLUMN is_default BOOLEAN NOT NULL DEFAULT FALSE;

-- 一个人最多一个默认。部分唯一索引而不是 CHECK：CHECK 管不了跨行的约束，
-- 而没有这道闸，"设为默认"忘了先清掉旧的就会出现两个默认，然后发信挑哪个
-- 又变成随机的——正是这一整批改动要消灭的那种毛病。
CREATE UNIQUE INDEX mail_accounts_one_default_idx
    ON mail_accounts (tenant_id, employee_id) WHERE is_default;

-- 存量每人一行，那一行就是他的默认。
UPDATE mail_accounts SET is_default = TRUE;

-- +goose Down

DROP INDEX IF EXISTS mail_accounts_one_default_idx;
ALTER TABLE mail_accounts DROP COLUMN is_default;

-- 回滚前要先手工处理掉一人多行的数据，否则这句加不回去。这是放宽约束的
-- 固有代价：放开容易，收回难。
ALTER TABLE mail_accounts ADD CONSTRAINT mail_accounts_tenant_id_employee_id_key
    UNIQUE (tenant_id, employee_id);
