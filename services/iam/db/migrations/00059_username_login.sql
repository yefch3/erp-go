-- +goose Up
-- 用户名登录：用户名在整套部署里唯一，不只是公司内唯一。
--
-- 客户要的开户方式是「管理员手动填用户名和密码，员工拿来登录」，账号不必是
-- 邮箱、不必绑邮箱。users.username 这一列早就在了（UNIQUE (tenant_id, username)），
-- 只是登录从来没认过它。
--
-- 为什么要跨租户唯一：登录页**没有「选公司」这一步**，这是有意的（一个页面
-- 服务所有公司）。邮箱登录靠地址本身全局唯一，用户名要做到同样的事只能靠这
-- 条索引。真正的部署是一家公司一套，这条索引在那儿等于没有约束；只在一套库
-- 里跑着多家（比如现在的测试租户）时它才会说话。
--
-- lower()：登录时不分大小写找人，「ZhangSan」和「zhangsan」不能是两个账号。
-- 上线前查过生产：22 个用户名零冲突。
CREATE UNIQUE INDEX users_username_lower_idx ON users (lower(username));

-- +goose Down
DROP INDEX IF EXISTS users_username_lower_idx;
