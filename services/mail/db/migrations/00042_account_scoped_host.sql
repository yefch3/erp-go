-- +goose Up

-- 收发服务器配置从「一家公司一套」搬到「一个信箱一套」。
--
-- 起因是一个人要能绑好几个信箱，而且可能跨服务商——263 + 163 + Gmail。
-- 今天这做不到，卡住的不是「一人一箱」那条唯一约束（那是下一期的事），
-- 而是这里：mail_hosts 的主键是 tenant_id，**整家公司只有一份 SMTP/IMAP
-- 地址**。ForSender 无条件把那一行拼进凭据（mailaccount.go），于是在一家
-- 263 的公司里，拿 Gmail 地址去绑只会拿着 Gmail 的账号去登 imap.263.net。
--
-- 搬完之后 mail_accounts 自己就是完整的连接凭据：地址、登录名、密文、
-- 主机、端口、加密方式，一行齐了。
--
-- **mail_hosts 这一版不动。** 部署是先迁库后换容器，中间那几秒旧容器还在
-- 服务，它读的还是那张表；而且按仓库的两版发布规矩（scripts/check-migration-safety.sh），
-- 删表是下一版的事。这一版它降级成「新建信箱时的默认值模板」——加信箱的
-- 表单拿它当预填值，不再是发信的事实来源。
--
-- 回填用 CROSS JOIN 而不是 LEFT JOIN：没有 mail_hosts 行的租户，它的账号
-- 就该保持默认值（空主机），那种账号今天本来也发不出信（ForSender 会返回
-- ErrMailHostNotConfigured）。
ALTER TABLE mail_accounts
    ADD COLUMN domain        VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN smtp_host     VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN smtp_port     INT          NOT NULL DEFAULT 465,
    ADD COLUMN smtp_security VARCHAR(8)   NOT NULL DEFAULT 'SSL'
        CHECK (smtp_security IN ('SSL','STARTTLS','NONE')),
    ADD COLUMN imap_host     VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN imap_port     INT          NOT NULL DEFAULT 993,
    ADD COLUMN imap_security VARCHAR(8)   NOT NULL DEFAULT 'SSL'
        CHECK (imap_security IN ('SSL','STARTTLS','NONE')),
    -- 配额也跟着搬：263 的套餐上限和 Gmail 的 500/天 完全不是一个量纲，
    -- 一个人同时用两家时，一份租户级配额说不出话。
    ADD COLUMN hourly_quota  INT          NOT NULL DEFAULT 100 CHECK (hourly_quota > 0),
    ADD COLUMN daily_quota   INT          NOT NULL DEFAULT 500 CHECK (daily_quota > 0);

UPDATE mail_accounts a
   SET domain        = h.domain,
       smtp_host     = h.smtp_host,
       smtp_port     = h.smtp_port,
       smtp_security = h.smtp_security,
       imap_host     = h.imap_host,
       imap_port     = h.imap_port,
       imap_security = h.imap_security,
       hourly_quota  = h.hourly_quota,
       daily_quota   = h.daily_quota
  FROM mail_hosts h
 WHERE h.tenant_id = a.tenant_id;

-- +goose Down

ALTER TABLE mail_accounts
    DROP COLUMN domain,
    DROP COLUMN smtp_host,
    DROP COLUMN smtp_port,
    DROP COLUMN smtp_security,
    DROP COLUMN imap_host,
    DROP COLUMN imap_port,
    DROP COLUMN imap_security,
    DROP COLUMN hourly_quota,
    DROP COLUMN daily_quota;
