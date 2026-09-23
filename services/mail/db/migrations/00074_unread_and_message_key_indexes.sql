-- +goose NO TRANSACTION
-- +goose Up

-- 两条查询各缺一个索引，2026-09-23 在生产上 EXPLAIN 出来都是全表扫。

-- 一、未读数（CountUnread、CountUnreadByMailbox）。
--
-- 每次开列表、每次切信箱、每来一封推送都要数一次。条件是「这个人的、没读
-- 的、没退信没归档没删、在收件箱或被捞回来的垃圾」，而能用的索引只有
-- (tenant_id, owner_id, received_at)：得把这个人的每一封信都回表看一遍
-- is_read。一个人两万多封的时候，规划器干脆整表扫——生产上老板那个账号
-- 就是这样，他一个人占了 mail 库一半的信。
--
-- **局部索引，谓词和两条查询的条件一字不差。** 规划器只在查询条件能推出
-- 索引谓词时才用它，改了查询条件却没改这里，索引就静悄悄地作废了——所以
-- 两条查询旁边都写着「口径见 00074」。索引里只有「此刻没读的信」，通常
-- 几百行，数一次是几百行的索引扫描，不是两万行回表。
--
-- 代价：is_read、folder 这些列出现在谓词里，改它们的 UPDATE 不再是 HOT
-- 更新。标已读一次一行，比数未读少得多，划算。
CREATE INDEX CONCURRENTLY IF NOT EXISTS email_inbound_unread_idx
    ON email_inbound (tenant_id, owner_id, account_id)
    WHERE NOT is_read AND NOT is_bounce
      AND archived_at IS NULL AND deleted_at IS NULL
      AND (folder = 'INBOX' OR (folder = 'JUNK' AND not_junk));

-- 二、打开追踪像素（FindMessageByKeyAnyTenant）。
--
-- 像素是收件人的邮件客户端来取的，不带会话，也就不知道是哪家公司——只有
-- message_key。已有的唯一索引是 (tenant_id, message_key)，首列用不上。这个
-- 地址是公开的，扫描器每来一次都是一次全表扫。
CREATE INDEX CONCURRENTLY IF NOT EXISTS email_messages_message_key_idx
    ON email_messages (message_key);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS email_messages_message_key_idx;
DROP INDEX CONCURRENTLY IF EXISTS email_inbound_unread_idx;
