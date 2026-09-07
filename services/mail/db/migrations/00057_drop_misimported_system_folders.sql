-- +goose Up
-- 00056 上线后第一次列文件夹，把服务器自带的系统文件夹（263 的草稿箱、已归档，
-- 网易的病毒文件夹）当成用户建的登记进了 mail_folders：导入只认英文名和
-- special-use 属性，这些服务器不声明属性、名字是中文。表现是左栏多出一个
-- 「草稿箱」，改名时服务器答 "can't rename default folder"。
--
-- 代码已经按各家系统名单跳过它们；这里把已经登记进去的清掉。只删里面没有
-- ERP 信的：真有信在里面的（用户挪进去过），留着比让信失去文件夹强。
DELETE FROM mail_folders f
WHERE lower(f.host_name) IN (
    '收件箱', '草稿箱', '草稿', '已发送', '已发送邮件', '发件箱',
    '已删除', '已删除邮件', '已删除的邮件', '垃圾邮件', '垃圾箱', '邮件回收站',
    '已归档', '归档', '归档邮件', '已存档',
    '病毒文件夹', '病毒邮件', '广告邮件', '订阅邮件', '通知邮件', '待办邮件',
    '星标邮件', '其他文件夹', '记事本', '便签',
    'inbox', 'drafts', 'draft', 'sent', 'sent messages', 'sent items', 'sent mail', 'outbox',
    'deleted', 'deleted messages', 'deleted items', 'trash', 'junk', 'junk e-mail', 'junk email',
    'spam', 'bulk mail', 'archive', 'archives', 'notes', 'templates', 'all mail',
    'important', 'starred', 'flagged', 'virus'
)
AND NOT EXISTS (
    SELECT 1 FROM email_inbound i
    WHERE i.tenant_id = f.tenant_id AND i.account_id = f.account_id
      AND i.folder = f.host_name AND i.deleted_at IS NULL
);

-- +goose Down
-- 删掉的是误登记的记录，服务器上的文件夹本来就在；回退不需要恢复它们。
SELECT 1;
