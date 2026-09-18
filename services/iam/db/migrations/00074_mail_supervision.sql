-- +goose Up

-- 员工邮箱监管（2026-09-18）：老板端按「国家 → 员工 → 收件箱 / 已发送」
-- 查看员工的来往邮件。
--
-- **单独一条权限，不挂在 mail:email:read 上。** 读自己的邮件和读别人的邮件
-- 是两件事：公司里每个人都有前者，后者只该有很少几个人有。挂在一起的话，
-- 这个功能上线那一刻，全公司都拿到了看同事邮件的能力，而且没有任何一步
-- 需要有人点头。
--
-- 看日志是**第三条**权限。能翻登记簿的人和能被登记的人分开，和导出那一对
-- （mail:email:export / mail:export:audit）同一个道理：一个人如果既是唯一
-- 会去看日志的人、又是日志记的对象，那本日志只是名义上的。
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('mail:supervision:read', '查看员工邮箱', 'mail', '/mail/supervision'),
  ('mail:supervision:audit', '查看邮箱监管记录', 'mail', '')
ON CONFLICT (code) DO NOTHING;

-- 只给 BOSS。别的角色要用，由管理员在角色页面自己勾——这正是"能单独授、
-- 单独收"的意思。
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT r.tenant_id, r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.code = 'BOSS'
  AND p.code IN ('mail:supervision:read', 'mail:supervision:audit')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions
WHERE permission_id IN (
  SELECT id FROM permissions WHERE code IN ('mail:supervision:read', 'mail:supervision:audit'));
DELETE FROM permissions
WHERE code IN ('mail:supervision:read', 'mail:supervision:audit');
