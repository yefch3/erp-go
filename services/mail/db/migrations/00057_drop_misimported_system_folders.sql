-- +goose Up
-- 00056 上线后第一次列文件夹，把服务器自带的系统文件夹（263 的草稿箱、已归档，
-- 网易的病毒文件夹）当成用户建的登记进了 mail_folders：导入只认英文名和
-- special-use 属性，这些服务器不声明属性、名字是中文。表现是左栏多出一个
-- 「草稿箱」，改名时服务器答 "can't rename default folder"。
--
-- 代码已经按各家系统名单跳过它们；这里把已经登记进去的清掉。只删里面没有
-- ERP 信的：真有信在里面的（用户挪进去过），留着比让信失去文件夹强。
-- 只删生产库里实际观察到的那四个名字（2026-09-06 查过：六条登记全是它们，都在
-- 首次列文件夹的同一分钟登记的，没有用户自建的撞名）。不按整份名单删：一个
-- 用户改版前正当建的同名文件夹一旦被删，导入不会再把它捡回来、同名又不让
-- 重建，它就从 ERP 里静默消失了。
DELETE FROM mail_folders f
WHERE f.host_name IN ('草稿箱', '已归档', '病毒文件夹', '病毒邮件')
AND NOT EXISTS (
    SELECT 1 FROM email_inbound i
    WHERE i.tenant_id = f.tenant_id AND i.account_id = f.account_id
      AND i.folder = f.host_name AND i.deleted_at IS NULL
);

-- +goose Down
-- 删掉的是误登记的记录，服务器上的文件夹本来就在；回退不需要恢复它们。
SELECT 1;
