-- +goose Up
-- 「发送后自己往已发送里留一份副本」从猜的变成一个开关。
--
-- 发完信往 已发送 里 APPEND 一份，是每个 IMAP 客户端的标准动作（Outlook、
-- Foxmail、Apple Mail 都这么做，也都为它带一个开关）。麻烦在于有些服务器
-- **自己也存一份**，两边都存就是两封一模一样的信躺在客户真实的邮箱里。
--
-- 原来这件事靠一份硬编码名单猜（只有 Gmail 在里面），生产上证明猜不准：
-- 同一个 smtp.263.net，yy@aaaindustryinc.com 发 36 封重 36 封，
-- erptest@263.net 发 11 封一封没重——263 把「保存客户端发信」做成了**每个
-- 信箱各自的**后台开关，不是服务商的属性。IMAP 的能力清单里问不出来，
-- 主机名也猜不出来。
--
-- **可空是有意的，三个值各有含义**：
--   NULL  还没人表过态 → 仍按 hostFilesItsOwnSentCopy 猜（今天的行为）
--   true  用户说「你来存」
--   false 用户说「服务器会存，你别存」
--
-- 所以不需要回填：装上之后每一行都是 NULL，行为和今天逐位相同。也因此
-- 换服务商时不必去动它——没表过态的会自动跟着新主机重新猜，表过态的不被
-- 覆盖。用一个布尔加一个「改过没有」的标记也能做到，但那是两列存一件事。
ALTER TABLE mail_accounts
    ADD COLUMN keep_sent_copy BOOLEAN;

-- +goose Down
ALTER TABLE mail_accounts DROP COLUMN keep_sent_copy;
