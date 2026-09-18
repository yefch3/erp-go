-- +goose Up

-- 谁，在什么时候，看了谁的邮箱。
--
-- 老板端的员工邮箱监管（2026-09-18）开的是一条**新的读信路径**：从前读一封
-- 信要两样东西——信是你自己的（owner_id 对得上），和一把你亲手用密码换来的
-- 信箱令牌。监管这条路两样都没有，靠的是一条权限。
--
-- 一条能读别人来往信件的权限，如果不留痕，就等于公司里有一扇谁都不知道被
-- 开过多少次的门。这张表就是那扇门上的登记簿：**每一次列表、每一次打开
-- 具体某封信，都写一行**。写不进去就不给看（和导出那条一样，见
-- mail_export_log），否则"留痕"只是句空话。
--
-- 名字两头都存了一份。看的人和被看的人都可能离职，而这张表恰恰是在他们
-- 离职之后被问起的。
CREATE TABLE mail_supervision_log (
    id          BIGSERIAL    PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL DEFAULT 1,

    -- 谁在看。
    viewer_id   BIGINT       NOT NULL,
    viewer_name VARCHAR(200) NOT NULL DEFAULT '',

    -- 看的是谁的箱。
    target_id   BIGINT       NOT NULL,
    target_name VARCHAR(200) NOT NULL DEFAULT '',

    -- LIST = 翻了某个文件夹的列表；OPEN = 点开了具体一封信。
    action      VARCHAR(16)  NOT NULL CHECK (action IN ('LIST', 'OPEN')),
    -- LIST 时是文件夹（inbox / sent）；OPEN 时是这封信的主题。
    --
    -- 主题写进来是有代价的，和导出日志那张表同一个取舍：能读这份日志的人
    -- 因此看得到一些主题行。不写的话，半年后问起"他到底看了什么"，答案是
    -- 一串邮件号，而那时信可能已经删了——那正是有人会问这个问题的时候。
    detail      TEXT         NOT NULL DEFAULT '',

    -- 从哪儿看的。只和网关的代理配置一样可信（见 TRUST_PROXY_HEADERS），
    -- 单独不作数；但"在公司里看的"和"凌晨两点从一个没见过的地址看的"是
    -- 这类日志第一个被问到的差别，而且事后补不出来。
    client_ip   VARCHAR(64)  NOT NULL DEFAULT '',

    viewed_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 两个问题：最近谁看了什么，和"这个人的箱被谁看过"。都按时间倒着答。
CREATE INDEX mail_supervision_log_at_idx
    ON mail_supervision_log (tenant_id, viewed_at DESC);
CREATE INDEX mail_supervision_log_target_idx
    ON mail_supervision_log (tenant_id, target_id, viewed_at DESC);

-- +goose Down
DROP TABLE mail_supervision_log;
