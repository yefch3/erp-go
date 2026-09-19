-- +goose Up

-- 员工负责哪个国家的市场（2026-09-18 老板要的）。
--
-- 用在员工邮箱监管那棵树上：国家 → 员工 → 收件箱 / 已发送。原来的分法是
-- 「按他名下客户所在的国家」，一个人名下客户分布在几个国家就出现在几个
-- 国家下面，而且没分到客户的人只能进「未分配」。老板要的是**员工自己**
-- 挂在哪个国家，就在哪个国家下面——那是一条资料，不是一条推算。
--
-- 两位国家码（ISO 3166-1 alpha-2），空串 = 没填，树上进「未分配国家」。
-- 名字不存：三种语言的国家名浏览器自带（前端 lib/countries）。
ALTER TABLE employees
  ADD COLUMN country_code VARCHAR(2) NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE employees DROP COLUMN IF EXISTS country_code;
