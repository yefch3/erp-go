-- +goose Up
-- 文件夹有角色了：左栏按同一级别显示服务器上的全部文件夹，能改名删除的只有
-- 用户自己建的（CUSTOM）。以前系统文件夹不登记、靠代码写死的列表显示，自建的
-- 才登记；分不清的就成了空壳（263 的草稿箱、网易的病毒文件夹）。现在每次列
-- 文件夹把服务器 LIST 回来的全部登记进来，角色按可信度定：服务器声明的属性 →
-- 我们定位到的已发送/垃圾/已删除/归档 → 各家默认名 → 都不是才算自建。
--
--   INBOX / SENT / JUNK / TRASH / ARCHIVE / DRAFTS：服务器自带、ERP 认得的
--   SYSTEM：服务器自带、ERP 不认得（病毒文件夹、广告邮件、其他文件夹）
--   CUSTOM：用户建的
ALTER TABLE mail_folders ADD COLUMN role VARCHAR(16) NOT NULL DEFAULT 'CUSTOM';
CREATE INDEX mail_folders_role_idx ON mail_folders (tenant_id, account_id, role);

-- 00057 只清了实际观察到的四个名字，这两条是别的同事的箱后来登记的，同样是
-- 服务商自带的。标成系统，不删：角色模型下它们本来就该在表里。
UPDATE mail_folders SET role = 'SYSTEM' WHERE host_name IN ('广告邮件', '其他文件夹');

-- +goose Down
DROP INDEX IF EXISTS mail_folders_role_idx;
ALTER TABLE mail_folders DROP COLUMN IF EXISTS role;
