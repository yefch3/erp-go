-- +goose Up

-- 收信登录被服务器拒绝的起始时间。空 = 眼下没被拒。
--
-- 为什么不直接用 auth_failed：那一位是给页面横幅用的，发信那条路也写它，
-- 而且发信时连超时、断线、对方限流都会写成 true（adapter/provider/smtp.go）。
-- 拿它来决定「后台还收不收这个箱的信」的话，发一封信时网络抖一下，这个箱
-- 就一整天不自动收信了（2026-09-23 审查发现）。这一列只由收信那几条路在
-- IMAP 登录被拒时写，只在收信登录成功、或者人重新填了凭据时清。
--
-- 记的是「从什么时候开始被拒」，不是「最近一次被拒」：重试的间隔按被拒了
-- 多久分档（见 ListMailboxesDueForStatus），刚被拒的勤一点试——服务商偶尔
-- 把「登录太频繁」也报成密码错——拒了一天以上的一天试一次。
ALTER TABLE mail_accounts ADD COLUMN login_rejected_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE mail_accounts DROP COLUMN login_rejected_at;
