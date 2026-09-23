-- +goose Up

-- 收信登录被服务器拒绝的起始时间。空 = 眼下没被拒。
--
-- 为什么不直接用 auth_failed：那一位是给页面横幅用的，发信那条路也写它，
-- 而且发信时连超时、断线、对方限流都会写成 true（adapter/provider/smtp.go）。
-- 拿它来决定「后台还收不收这个箱的信」的话，发一封信时网络抖一下，这个箱
-- 就一整天不自动收信了（2026-09-23 审查发现）。这一列只由收信那几条路在
-- IMAP 登录被拒时写，只在收信登录成功、或者人重新填了凭据时清。
--
-- 有值的箱，后台一律不再拿这份凭据去登录：不轮询、不问轻状态、不守常开
-- 连接、不补原件、不跑写回队列。只剩两个口子：员工本人打开邮箱页时的那次
-- 收信（人在场，看得到横幅；服务商把「登录太频繁」误报成密码错的话，那一
-- 次就恢复了），以及员工重新填凭据。两者成功时都会清掉这一列。
--
-- 记的是「从什么时候开始被拒」（COALESCE，不随后续失败后移），排查时一眼
-- 看得出这个箱断了多久。
ALTER TABLE mail_accounts ADD COLUMN login_rejected_at TIMESTAMPTZ;

-- 写回队列里每条操作记下入队时那个文件夹的 UIDVALIDITY（服务器给这批编号
-- 的「版本号」）。0 = 不知道（这一列之前入队的、或那个文件夹还没同步过）。
--
-- 为什么现在要：上面那一列让被拒的箱的写回**暂停**等凭据修好，暂停可能很久。
-- 而写回按 UID 执行——如果这期间邮箱搬过家（换服务商、服务器重建），UID 全
-- 部重排，几周前那条「把 UID 5 挪进回收站」落到的是另一封信。从前写回失败
-- 二十次（三四个小时）就作废，这个窗口有上限；暂停之后没有了（2026-09-23
-- 审查发现）。执行前拿这一列和服务器当前的 UIDVALIDITY 比，对不上就作废，
-- 不去动任何一封信。
ALTER TABLE mail_flag_ops ADD COLUMN uid_validity BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE mail_flag_ops DROP COLUMN uid_validity;
ALTER TABLE mail_accounts DROP COLUMN login_rejected_at;
