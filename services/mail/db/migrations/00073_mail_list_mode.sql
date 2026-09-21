-- +goose Up

-- 列表按会话合并，还是一封一行——每个人自己说了算。
--
-- 我们的收件箱从一开始就按会话合并（Gmail 那一派）。客户里有相当一部分人
-- 是用 263 和 Foxmail 用惯的，那两家默认一封一行，于是「同一个客户回了三次
-- 怎么只有一行」在他们看来是信丢了。
--
-- 两种都不是错的，所有正经邮件客户端也都没替用户定死：Gmail、Outlook、
-- Foxmail 各有一个开关。这张表就是那个开关。
--
-- **跟人走，不跟公司走**，和签名、模板同一条口径（00069 / 00070）：看信的
-- 方式是个人习惯，隔壁桌的人不该被我的选择影响。
--
-- 为什么单开一张表而不是在 employees 上加一列：员工表在 iam 服务的库里，
-- 邮件服务读不到它，跨服务去问一次只为拿一个布尔值不划算。而这类「邮箱
-- 里的个人偏好」往后大概率还会有第二个第三个，一张表加一列比新开一张表
-- 便宜。
CREATE TABLE mail_list_prefs (
    -- tenant_id 没有默认值也不能是 0，见 00072。
    tenant_id   BIGINT NOT NULL CHECK (tenant_id > 0),
    employee_id BIGINT NOT NULL,
    -- THREAD = 按会话合并（一直以来的样子，也是没有这一行时的含义）。
    -- MESSAGE = 一封一行。
    --
    -- 存字符串不存布尔：这个开关将来完全可能出现第三档（比如 Outlook 那种
    -- 「只在某些文件夹里合并」），而一个叫 thread_mode 的布尔到那天就得改
    -- 类型——那是一条 ALTER COLUMN TYPE，正是部署守卫拦着的那种迁移。
    list_mode   TEXT NOT NULL DEFAULT 'THREAD' CHECK (list_mode IN ('THREAD', 'MESSAGE')),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- 一人一行。没有行 = 没改过 = THREAD，所以读取那一侧永远不需要先插一行。
    PRIMARY KEY (tenant_id, employee_id)
);

-- +goose Down

DROP TABLE IF EXISTS mail_list_prefs;
