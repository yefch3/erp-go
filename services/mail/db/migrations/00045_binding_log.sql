-- +goose Up

-- 绑定邮箱的留痕。
--
-- 这张表是和「地址交还给调用方」同一批加的，它是那次放宽的**对价**。
--
-- 在此之前，一个人能绑的地址只有一个：他登录 ERP 用的那个。地址不是请求
-- 体里的字段，所以「谁绑了什么」不必记——答案永远是「他自己那个」。多信箱
-- 把地址交回给了调用方，那个答案就没了，于是必须有地方写下来。
--
-- 要能回答的问题，是出事时才会有人问的那几个：
--
--   · 这个箱是谁绑上去的、什么时候
--   · 有没有人反复拿别人的地址试（撞 UNIQUE (tenant_id, email) 会失败，
--     失败也记——只看成功的话，探测行为正好是看不见的那一半）
--   · 某个地址曾经属于谁
--
-- 只记不拦：这里没有任何权限判断，围栏在网关和服务层。
CREATE TABLE mail_binding_log (
    id          BIGSERIAL   PRIMARY KEY,
    tenant_id   BIGINT      NOT NULL DEFAULT 1,
    employee_id BIGINT      NOT NULL,
    -- 绑成了才有账号 id。失败的那几条是空的，而失败恰恰是最值得留的。
    account_id  BIGINT,
    email       VARCHAR(320) NOT NULL,
    -- 挑的哪家服务商，或者 'other'（自己填的主机）。
    provider    VARCHAR(32) NOT NULL DEFAULT '',
    -- BIND 新绑 · REBIND 同一个地址重填授权码 · FAILED 没绑上
    -- · DEFAULT 改默认发件箱 · UNBIND 解绑
    action      VARCHAR(16) NOT NULL,
    -- 失败的原因，原样存服务返回的那句话。成功时为空。
    detail      TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 两条查法：看一个人绑过什么，看一个地址被谁碰过。后者是查探测行为用的，
-- 所以地址那条不带 employee_id。
CREATE INDEX mail_binding_log_employee_idx
    ON mail_binding_log (tenant_id, employee_id, id DESC);
CREATE INDEX mail_binding_log_email_idx
    ON mail_binding_log (tenant_id, email, id DESC);

-- +goose Down

DROP TABLE IF EXISTS mail_binding_log;
