-- +goose Up

-- 邮箱地址一律存小写。
--
-- 起因是绑定入口把地址交还给了调用方（00045 那一批）。地址从此是人手输入
-- 的，而人会打 Lina@Sunrise.com；从前它来自登录令牌，是什么样就是什么样，
-- 也没有第二个地方拿地址去查。
--
-- 现在有两个地方按地址找行：
--
--   · UpsertMailAccountShell 的 ON CONFLICT (tenant_id, email)
--   · GetMailAccountByEmail
--
-- 这两个**必须用同一个口径**，否则会出现一种不报错的坏法：查的时候
-- lower() 匹配上了老行，插的时候 ON CONFLICT 按原样比对不上，于是同一个
-- 信箱变成两行——两份凭据、两份同步游标、两套已收邮件，而唯一约束一声
-- 不吭（'Me@x.com' 和 'me@x.com' 在它眼里就是两个不同的值）。
--
-- 统一到小写而不是给约束套 lower()：小写是**存进去的时候**就定好的，
-- 查询直接 email = $1 走得上 mail_accounts_tenant_id_email_key 那条索引。
-- 套 lower() 的话得再建一条表达式索引，而且唯一约束仍然是大小写敏感的
-- ——两个大小写变体照样能共存，那时按 lower() 查是 sqlc 的 :one，
-- pgx 读到第一行就返回，挑中哪一个是随机的。
--
-- **这一句撞上唯一约束时会让迁移失败，那是有意的。** 失败意味着库里真的
-- 有两行地址只差大小写——那是两个人（或一个人两次）绑了同一个信箱，谁的
-- 邮件该归谁需要人来定，不该由一条 UPDATE 悄悄留下一个、覆盖另一个。
UPDATE mail_accounts SET email = lower(email), updated_at = now()
 WHERE email <> lower(email);

-- +goose Down

-- 大小写还不回去：原来是什么样没有地方记着。这是有损的一次规范化，
-- 而它无损的那一半是——回滚之后代码读的仍然是小写地址，照样能用。
SELECT 1;
