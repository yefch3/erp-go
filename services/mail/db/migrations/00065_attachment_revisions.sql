-- +goose Up
-- 附件在浏览器里被改过之后，改动存这里。
--
-- **原件永远不动。** 这是方晨拍的板，理由不是技术：客户发来的那份单据是
-- 将来对账、扯皮时唯一能拿出来的东西。覆盖掉它省下的那点事，换不回一次
-- 「我们手里没有你原本发的版本」。
--
-- 所以 email_inbound_attachments.file_key 从此只有一个意思：**客户发来的
-- 原件**。改过的每一版在这张表里各占一行，各有自己的对象；第 1 版是第一次
-- 改出来的，原件不入表（它就是「第 0 版」，但那是一句话，不是一行数据）。
--
-- 为什么是一张表而不是在附件行上加一个 latest_file_key：
--
--   一、改第二次、第三次是常态（报价来回改），一行只留最新的话，中间那几版
--       就不见了——而「上一版我给的是什么价」正是会被问到的。
--   二、每一版要记是谁改的。一行放不下第二个人。
--
-- version 从 1 起，(tenant_id, attachment_id, version) 唯一：并发时两个人同时
-- 存成同一版会撞在这个索引上，第二个重算版本号再来，而不是安静地互相覆盖。
CREATE TABLE mail_attachment_revisions (
    id            BIGSERIAL    PRIMARY KEY,
    tenant_id     BIGINT       NOT NULL,
    -- 附件没了（整封信被清掉），它的修改版跟着走：那些对象没有任何东西
    -- 指得到它们了。
    attachment_id BIGINT       NOT NULL REFERENCES email_inbound_attachments(id) ON DELETE CASCADE,
    -- 冗余一份，为了按信查「这封信里有哪些附件被改过」时不用先 JOIN 回去。
    inbound_id    BIGINT       NOT NULL,
    version       INT          NOT NULL CHECK (version >= 1),
    file_key      VARCHAR(512) NOT NULL,
    file_size     BIGINT       NOT NULL DEFAULT 0,
    -- 谁改的。ERP 的员工 id；在线编辑器里的署名用的也是它。
    edited_by     BIGINT       NOT NULL,
    edited_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, attachment_id, version)
);

-- 列表那一路问的是「这封信的附件有没有改过的版本、最新是第几版」，所以
-- 按信走一条，取最新那一版。上面那条唯一索引按 attachment_id 起头，答不了
-- 「一封信里所有附件」这个问题。
CREATE INDEX mail_attachment_revisions_mail_idx
    ON mail_attachment_revisions (tenant_id, inbound_id, attachment_id, version DESC);

COMMENT ON TABLE mail_attachment_revisions IS
    '收件箱附件在线编辑后的版本；原件在 email_inbound_attachments 上，永不覆盖';

-- +goose Down
DROP TABLE IF EXISTS mail_attachment_revisions;
