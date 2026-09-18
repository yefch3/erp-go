-- +goose Up

-- 邮件模板也只属于写它的人（2026-09-18，和签名同一天、同一个口径）。
--
-- 模板原来也分「公司统一」（owner_type = 'TENANT'，owner_id = 0）和「仅自己」
-- 两层。老板定的是：签名和模板都是各人自己的数据，别人看不到。做法和
-- 00069 一样：删掉 TENANT 行（没人可以接手），列留到下一版再删。
-- 改到这天生产库里 11 条模板全是个人的，这一句在生产上是零行。
DELETE FROM email_templates WHERE owner_type = 'TENANT';

-- +goose Down

-- 删掉的行回不来；列没动，没什么可退的。
