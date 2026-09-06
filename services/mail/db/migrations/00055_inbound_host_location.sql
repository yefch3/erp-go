-- +goose Up

-- 信被挪走之后，服务器上那份此刻在哪。
--
-- (folder, imap_uid) 是这封信入库时在服务器上的位置，也是同步用的身份。删除
-- 或归档会把服务器上那份挪进「已删除」/「归档」，它在那里拿到一个**新的**
-- UID——而我们的行还指着老位置。之后要碰它（彻底删除、恢复、对账里问一句
-- "还在回收站吗"）都得先把它**找回来**，从前靠 UID SEARCH HEADER Message-Id。
-- 263 不认这种搜索（"can't search that criteria"），所以 263 上彻底删除和恢复
-- 一直是坏的。
--
-- 支持 UIDPLUS 的服务器（我们接的每一家都是）在 MOVE/COPY 的应答里会带
-- [COPYUID 世代 旧UID 新UID]。接住它、记在这两列里，之后就直接按 UID 操作，
-- 不再需要搜索。0 / 空 = 没记录（老数据，或者服务器没给），走搜索兜底。
ALTER TABLE email_inbound ADD COLUMN host_folder TEXT   NOT NULL DEFAULT '';
ALTER TABLE email_inbound ADD COLUMN host_uid    BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE email_inbound DROP COLUMN host_uid;
ALTER TABLE email_inbound DROP COLUMN host_folder;
