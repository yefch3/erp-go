-- +goose Up

-- 收件人整段存下来，不再只留第一个。
--
-- to_email 从建表起就是一个地址（VARCHAR(320)，正好是一个邮箱地址的上限），
-- 解析时取的是 To 里的第一个。客户一封信群发给公司七个人，ERP 里只记下
-- 第一个，详情页「收件人」就只有那一个；而「回复全部」要是建在这份数据上，
-- 也只会带上那一个人。抄送（cc）倒是从 00038 起整段存着——两个头一个整存
-- 一个只留头一个，没有道理。
--
-- 这一列存**解码后的原头**（"Ana <a@x>, b@y, ..."），和 cc 同一个样子：
-- 显示时原样给人看，「回复全部」要地址时再解析。to_email 留着不动：它是
-- 「第一个收件人」，已发送列表那一列、按收件人排序、搜索文本都还靠它。
--
-- 存量由 RunToAllBackfill 从对象存储里的原件补回来（原件一直在，这正是它
-- 存在的理由之一）；补之前这一列是空串，读的那一侧空则退回 to_email。
ALTER TABLE email_inbound ADD COLUMN to_all TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE email_inbound DROP COLUMN to_all;
