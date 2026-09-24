-- +goose NO TRANSACTION
-- +goose Up

-- 回信引用缓存下来的图时，AttachmentsByKeys 按 object_key 问库。00077 只给
-- 正文自带的图建了局部索引；从这一版起发件人网站的副本也走这条查询（回信
-- 里原来发出去的是一条一小时就过期的地址），局部索引盖不住了，换成整列的。
-- 这张表生产上约 22 万行，建一次很快；CONCURRENTLY 不挡写入。
CREATE INDEX CONCURRENTLY IF NOT EXISTS email_inbound_images_object_key_idx
    ON email_inbound_images (object_key);
DROP INDEX CONCURRENTLY IF EXISTS email_inbound_images_data_key_idx;

-- +goose Down
CREATE INDEX CONCURRENTLY IF NOT EXISTS email_inbound_images_data_key_idx
    ON email_inbound_images (object_key)
    WHERE source_url LIKE 'data:sha256,%';
DROP INDEX CONCURRENTLY IF EXISTS email_inbound_images_object_key_idx;
