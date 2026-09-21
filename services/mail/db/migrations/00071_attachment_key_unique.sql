-- +goose Up

-- 一个存储位置只许一行附件指着。
--
-- 2026-09-20 的事故：收件附件的存储位置从前按「信编号 + 文件名」算，而邮件
-- 客户端给内嵌图片起的名字就那么几个。一封转发的船期更新里，正文的船舶
-- 动态截图（149 KB）和签名 logo（4.3 KB）都叫 image.png，算出同一个位置；
-- 对象存储按位置覆盖写、不报错，后写的 logo 把截图盖掉，而截图那一行还
-- 指着它。生产上 461 组、1015 个文件被覆盖，909 个是正文内嵌图。
--
-- 位置公式已经加了序号（inboundAttachmentKey），被覆盖的文件也已经从原件
-- 重新解出来补回去了（repair-attachment-keys，346 封全部成功）。这一条是
-- 第三道：**让「两行指同一个位置」这件事根本写不进库**。公式再被谁改错，
-- 插第二行时当场报错，而不是悄悄毁掉一个文件——和 iam 00081 给角色行加
-- 复合外键是同一个思路。

-- ---- 1. 0 字节的附件行：位置清空。
--
-- 补数据时跳过的那三封（17433 / 37071 / 37242，55 行）全是营销邮件塞进来的
-- 空图片部件，每个 0 字节。它们**从来就没有内容被存过**，却共用着一个位置。
-- 这个代码库里「空的 file_key」的含义正是「从来没存过」（见 inbound.go 里
-- 超大附件那段），所以清空是把话说实，不是抹掉数据：名字、类型、大小三样
-- 照旧留在行上。
UPDATE email_inbound_attachments SET file_key = ''
WHERE file_size = 0 AND file_key <> '';

-- ---- 2. 万一同一封信里还有别的重名撞在一起（本地库、别的环境），保留最早
--         的一行，其余的位置清空——它们指着的内容本来就不是自己的。
--         生产上跑到这里是 0 行。
UPDATE email_inbound_attachments a SET file_key = ''
WHERE a.file_key <> ''
  AND EXISTS (SELECT 1 FROM email_inbound_attachments b
              WHERE b.inbound_id = a.inbound_id AND b.file_key = a.file_key AND b.id < a.id);

-- ---- 3. 约束本体：**按信**，不是全局。
--
-- 要钉住的不变量是「同一封信里两行不许指同一个文件」——这次毁掉文件的正是
-- 这个形状。做成全局唯一会顺带禁止两封信共用一个文件：那既不是这次的毛病，
-- 将来真想做「内容相同的附件只存一份」时还得先把它拆掉。
--
-- 空位置不参与：「从来没存过」可以有很多行（超大附件、0 字节的空部件）。
CREATE UNIQUE INDEX email_inbound_attachments_file_key_uniq
    ON email_inbound_attachments (inbound_id, file_key) WHERE file_key <> '';

-- +goose Down

DROP INDEX IF EXISTS email_inbound_attachments_file_key_uniq;
