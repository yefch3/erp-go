-- 收进来的邮件，除了正文之外还有几件事值得留下。
--
-- reply_to：真正的回信地址。它和 From 不一样时，「谁写的」和「点回复会发给谁」
-- 就是两个答案。商业邮件诈骗最常用的一手正是这个——伪造一封看似来自老供应商
-- 的邮件，把 Reply-To 换成自己的地址，业务员一点回复，货款信息就发给了骗子。
-- 存下来，界面才有可能在两者不一致时提醒。
--
-- cc：谁在这段对话里能看到。原样保留（含显示名），因为这本身是业务事实——
-- 对方把谁拉进来了、我们回信时该不该带上。
--
-- auth_spf / auth_dkim：收信服务器验过的身份，也就是 Gmail 显示的 mailed-by
-- 和 signed-by。它们和 From 的区别在于，From 是发信人自己写的、想写什么写什么，
-- 而这两个是收信方拿去 DNS 核对过的。只在验证通过时有值：没通过就什么都不是，
-- 不能把一个签名没证明的域名摆出来当依据。
--
-- 全部可空且有默认值：老邮件没有这些信息，而「不知道」和「没有」是两回事——
-- 界面对空值的说法是「未记录」，不是「没有回信地址」。回填要靠重新解析 S3 里
-- 的原始 MIME，那是另一件事。
--
-- migration-safety: 只加列且都有默认值，旧版本代码不写这几列也照常工作，
-- 回滚到上一个版本仍可运行。

-- +goose Up
ALTER TABLE email_inbound
    ADD COLUMN reply_to  VARCHAR(320) NOT NULL DEFAULT '',
    ADD COLUMN cc        TEXT         NOT NULL DEFAULT '',
    ADD COLUMN auth_spf  VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN auth_dkim VARCHAR(255) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE email_inbound
    DROP COLUMN reply_to,
    DROP COLUMN cc,
    DROP COLUMN auth_spf,
    DROP COLUMN auth_dkim;
