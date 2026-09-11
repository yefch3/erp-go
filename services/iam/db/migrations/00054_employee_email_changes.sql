-- +goose Up

-- 改登录邮箱：从「管理员直接写」变成「新地址收到信、点开才生效」。
--
-- 为什么需要这张表，而不是继续让 UpdateEmployeeDetails 写 email 那一列：
--
-- 登录同时看两样东西——employees.email 找到账号（00023 起全系统唯一），
-- employees.email_verified_at 证明这个信箱真的存在、真的是他的
-- （service.go 的登录会拒绝 email_verified_at 为空的账号）。而
-- UpdateEmployeeDetails 只写 email、**不碰 email_verified_at**：新地址凭空继承了
-- 旧地址挣来的那个「已验证」。
--
-- 后果不是理论上的。管理员把 alice@ 打成 aliec@，保存，系统一声不吭：
--   · alice 下次登录要用一个不存在的地址，而她并不知道
--   · 忘记密码的重置信会发到那个不存在的地址
--   · 谁都发现不了，因为没有任何一处会去问「这个信箱收得到信吗」
--
-- 同一个文件里的 SetEmployeeEmailVerified 才是对的形状：**地址和验证时间一起写**，
-- 因为那条路走过「这个地址真的收得到信」。这张表就是把改邮箱也接到那条路上。
--
-- 打错字的代价因此变成「信退回来了，什么都没改」——旧地址照常能登录。
CREATE TABLE employee_email_changes (
    id           BIGSERIAL    PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL,
    employee_id  BIGINT       NOT NULL REFERENCES employees(id),

    -- 要改成的新地址。链接发到这里。
    new_email    VARCHAR(200) NOT NULL,

    -- 发起变更时他的旧地址。
    --
    -- 和 employee_invitations.email 是同一个道理，方向相反：那张表存「链接发到哪」，
    -- 用来确认账号的地址没在链接飞行途中被人挪走；这里存「当时是从哪改」，
    -- 用来确认同一件事。两个管理员先后发起两次变更，或者中间有人直接改了地址，
    -- 兑换时旧地址对不上就拒绝——否则第一封信会把账号带到一个早就作废的目的地。
    old_email    VARCHAR(200) NOT NULL,

    -- sha256(token)，不是 token 本身。
    --
    -- 和 employee_invitations.token_hash 同样的理由：这张表里每一行活着的记录
    -- 都是一把能把某个账号的登录地址搬走的钥匙。备份、支持查询、日志里漏出去一次，
    -- 就是一批账号的接管。存哈希意味着存下来的东西打不开任何门。
    token_hash   BYTEA        NOT NULL UNIQUE,

    expires_at   TIMESTAMPTZ  NOT NULL,
    used_at      TIMESTAMPTZ,

    -- 谁发起的。出了事第一个要问的就是这个。
    requested_by BIGINT       NOT NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 一个人同时只能有一个待确认的变更。
--
-- 重发很常见（第一封进了垃圾箱、地址又打错了一次），而重发的朴素写法——再插一行——
-- 会让上一把钥匙继续活着。服务端在插入前先删旧行；这个索引让它成为规矩而不是习惯。
CREATE UNIQUE INDEX employee_email_changes_live
    ON employee_email_changes (tenant_id, employee_id)
    WHERE used_at IS NULL;

-- 同一个地址同时只能被一个人预约。**全局，不分公司**——employees.email 本身就是
-- 全系统唯一的（00023），所以两个人都预约 x@co.com 时，先点开的那个成功，
-- 后点开的那个会撞上 employees_email_key 报「工号已存在」（translateUnique 的老毛病），
-- 而那时信已经发出去了、人已经点过了，解释不清。
--
-- 在发起那一刻就拦住，报「这个地址已经有人在改了」，是能说清楚的唯一时机。
CREATE UNIQUE INDEX employee_email_changes_live_address
    ON employee_email_changes (lower(new_email))
    WHERE used_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS employee_email_changes;
