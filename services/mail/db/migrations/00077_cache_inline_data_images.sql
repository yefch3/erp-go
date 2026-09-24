-- +goose NO TRANSACTION
-- +goose Up

-- 正文自带的图片（<img src="data:image/...">，见 dataimages.go）从这一版起由
-- 图片缓存那一遍解出来存进对象存储，阅读时换成签名地址。原来它们一张都不
-- 显示（2026-09-24 清点：客户来信 128 封，多是 Foxmail 发来的截图）。

-- 一、已经过过缓存那一遍的旧信，带这种图的重新排进队列。
--
-- 缓存那一遍现在会跳过这封信已经存过的外链图片（cachedImageSources），所以
-- 重排一次不会把它们再取一遍——再取就是让对方服务器再记一次「被打开了」。
-- 当初没取到的外链图片会再试一次，这个认。只动 images_cached_at。
UPDATE email_inbound
SET images_cached_at = NULL
WHERE images_cached_at IS NOT NULL
  AND body_html ~* '<img[^>]*\ssrc\s*=\s*["'']?\s*data:image/';

-- 二、回信引用这种图时按 object_key 问库（AttachmentsByKeys 的第二段）。
-- 局部索引，谓词和那条查询一字不差；这种图全库一两百张，索引很小。
CREATE INDEX CONCURRENTLY IF NOT EXISTS email_inbound_images_data_key_idx
    ON email_inbound_images (object_key)
    WHERE source_url LIKE 'data:sha256,%';

-- +goose Down
-- 重新排队不回滚：缓存那一遍对已经存过的图什么都不做，多排一次无害。
DROP INDEX CONCURRENTLY IF EXISTS email_inbound_images_data_key_idx;
