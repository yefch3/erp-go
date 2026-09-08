-- +goose Up
-- 超大附件：装不下的文件改成下载链接，跟着正文走，不再塞进邮件本身。
--
-- 为什么需要它。附件必须 base64 编码才能走 SMTP，**体积涨三分之一**，而
-- 收件人那一侧的上限我们既读不到也控制不了——Gmail 是 25 MB（编码后的整封
-- 信），也就是实际文件约 18 MB 封顶。所以「把上限调高」解决不了任何问题：
-- 调高只是把「写信时当场拒绝」换成「发出去之后按收件人退信」，而后者是
-- 异步的、用户以为已经发出去了。
--
-- 所有邮件客户端的做法都一样，没有一家是靠邮件协议解决的，全都退回自己家
-- 的云盘：Outlook → OneDrive，Foxmail → 腾讯中转站，Gmail 网页版 → Drive。
-- 那个链接从来不是第三方邮箱给的。我们这边的对象存储本来就有，而且发信附件
-- 现在就是浏览器直传上去的——文件已经在该在的地方了，缺的只是一个公开取件口。
--
-- 两列，形状照抄 email_images（00002），因为要解决的是同一个问题：
--
--   token   随机、猜不到，公开地址里出现的就是它。只有真的转成链接的那些
--           文件才会拿到 token——没必要给每个附件都开一个公开取件口。
--   status  撤回之后停止服务，但**行留着**：客户问「你发我的那个链接打不开」
--           时得答得上来「是被撤回了」，而不是一句查无此物。同 email_images。
--
-- 不设过期。腾讯中转站 30 天到期正是被用户骂的点，QQ 邮箱 2025-03 把它升级
-- 成「文件云盘」不再过期。既然知道那条路的结局，就别再走一遍。
ALTER TABLE email_attachments
    ADD COLUMN token  VARCHAR(64),
    ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE','WITHDRAWN'));

-- 部分唯一：只有拿到 token 的行受约束。绝大多数附件的 token 是 NULL，
-- 它们之间不该互相冲突。
CREATE UNIQUE INDEX email_attachments_token_idx
    ON email_attachments (token) WHERE token IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS email_attachments_token_idx;
ALTER TABLE email_attachments DROP COLUMN token, DROP COLUMN status;
