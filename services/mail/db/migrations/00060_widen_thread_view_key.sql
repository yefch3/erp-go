-- +goose Up
-- 视图键装不下文件夹名。mail_thread_view.view 是 00034 定的 VARCHAR(16)，那时
-- 它只可能是 INBOX / JUNK / TRASH / ARCHIVE / STARRED 这几个词；00056 之后它还
-- 可能是 'F:' 加服务器上的文件夹名，而文件夹名允许 120 个字。
--
-- 后果不是显示不全，是**整条写入失败**：mail_thread_view 由 email_inbound 上的
-- 触发器维护，名字长于 14 个字的文件夹，往里挪信会直接报错，同步进来的信会被
-- 记一条 "could not store a message" 然后丢掉。生产上还没人起过这么长的名字，
-- 所以一直没露出来。
--
-- 改成 TEXT：group_key 已经是 255，view 没有理由更窄，而 Postgres 的 TEXT 和
-- varchar 存储完全一样、没有额外代价。
--
-- migration-safety: ALTER COLUMN view TYPE —— 只是放宽长度上限（varchar(16) →
-- text），旧代码读到的仍是同样的字符串，扫描不会失败；不放宽的话名字稍长的
-- 文件夹一挪信就报错。2026-09-07 确认。
ALTER TABLE mail_thread_view ALTER COLUMN view TYPE TEXT;

-- +goose Down
-- 回退要先删掉装不下的行，否则 ALTER 会失败。它们是缓存，下次列表会重建。
DELETE FROM mail_thread_view WHERE char_length(view) > 16;
ALTER TABLE mail_thread_view ALTER COLUMN view TYPE VARCHAR(16);
