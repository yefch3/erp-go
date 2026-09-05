-- +goose Up

-- last_error 旁边加一位：这次失败是不是凭据的问题。
--
-- 页面上的红横幅带一颗「重新登录邮箱」按钮，从前只要 last_error 非空就显示。
-- 可 last_error 里装的是**任何**同步错误——263 几分钟掐一次空闲连接，每掐
-- 一次员工就被劝去重输一遍授权码，而重输从来没修好过任何东西。
-- 「一直要重新登录」就是这么来的。
--
-- 错误文本是给人看的，不该拿来做判断；判断要靠一个专门的位。它由写
-- last_error 的同一处代码同时写：同步路径按错误类型判，发信路径本来就只在
-- 认证失败时才记。清 last_error 的时候一起清。
ALTER TABLE mail_accounts ADD COLUMN auth_failed BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE mail_accounts DROP COLUMN auth_failed;
