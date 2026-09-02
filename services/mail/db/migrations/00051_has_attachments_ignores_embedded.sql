-- +goose Up

-- 列表上那枚回形针不再为签名 logo 亮。
--
-- has_attachments 从前是「解析出来的部件数 > 0」，而正文里 <img src="cid:X">
-- 指着的那个部件——十有八九是签名的 logo——也算在内。读信页早就把它藏起来
-- 了（embedded.go 的 hideEmbedded），于是列表上亮着回形针、点进去什么附件
-- 都没有。用的人问过「明明没有附件，为什么有图标」。
--
-- 入库处已经改成同一条规矩（hasListedAttachments）；这里把存量补齐。只动
-- 现在是 TRUE 的行：这个标记从前只会多算、不会少算，所以只可能从 TRUE 变
-- FALSE。判断和 Go 那边同一条：部件有 Content-ID，且正文里出现 cid:<它>。
--
-- 00031 之前入库的附件行 content_id 是空的，那批由 RunContentIDBackfill 慢慢
-- 补；补上之前它们照旧算附件——宁可多亮一枚回形针，不把真附件算没。
--
-- 这条 UPDATE 会触发 mail_thread_view 的刷新（按语句、带过渡表），所以
-- 会话视图上的 any_attachment 跟着一起改，不用另写。
UPDATE email_inbound i
SET has_attachments = EXISTS (
    SELECT 1 FROM email_inbound_attachments a
    WHERE a.tenant_id = i.tenant_id
      AND a.inbound_id = i.id
      AND NOT (a.content_id <> ''
               AND position('cid:' || a.content_id IN i.body_html) > 0)
)
WHERE i.has_attachments;

-- +goose Down

-- 没有东西可退：旧值是「多算了签名 logo」，退回去等于把 bug 装回来，而且
-- 哪几行原来是 TRUE 也没记着。留空是有意的。
SELECT 1;
