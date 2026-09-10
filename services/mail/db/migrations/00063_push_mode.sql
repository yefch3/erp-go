-- +goose Up
-- 这个信箱靠推送（IDLE）还是靠轮询收信，记下来。
--
-- **这是一次有意的反转。** idledrop.go 上原本写着「不落库是有意的——这是一台
-- 服务器此刻的脾气，不是一条需要长期记住的事实」。反转的理由有两条，都不是
-- 当初那句话说错了，是当初没想到的：
--
--   一、没法查。「这个箱现在是推送还是轮询」只存在于进程内存里，问一次就得
--       去翻日志，而那几行日志只在有人正开着邮件页时才产生——没人看的时候
--       IDLE 循环根本不跑，日志一条都没有。业务问起来只能靠推断，这次就是
--       这么被问住的。
--   二、每次重启忘光。判断在内存里，所以每部署一次就重新学一遍：先被掐三圈
--       （约三分钟、三次重新登录），才退回轮询。平时一天发不了几次版无所谓，
--       密集发版的日子就是每次都吵一遍。
--
-- 「此刻的脾气」那句仍然成立，所以这里存的不是永久结论：连同判断时间一起存，
-- 过了冷静期照样重新试 IDLE。存的是「上次学到的」，不是「从此就是这样」。
--
-- 空串 = 还没学到。不用 NULL：这一列永远有确定的三态之一，而 NULL 在 Go 那边
-- 要多一层 pgtype 包装，换不来任何表达力。
ALTER TABLE mail_accounts
    ADD COLUMN push_mode       VARCHAR(16) NOT NULL DEFAULT '',
    ADD COLUMN push_checked_at TIMESTAMPTZ;

COMMENT ON COLUMN mail_accounts.push_mode IS
    '收信方式：IDLE=服务器推送，POLL=两分钟一轮的轮询，空=还没学到';
COMMENT ON COLUMN mail_accounts.push_checked_at IS
    '上一次判断出 push_mode 的时刻。过了冷静期会重新试 IDLE。';

-- +goose Down
ALTER TABLE mail_accounts
    DROP COLUMN IF EXISTS push_mode,
    DROP COLUMN IF EXISTS push_checked_at;
