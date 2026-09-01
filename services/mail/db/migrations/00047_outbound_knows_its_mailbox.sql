-- +goose Up

-- 出站的每一封信记住它是从**哪个信箱**发的。
--
-- 到今天为止这张表只有 sender_id（谁发的），没有 account_id（从哪个箱发的）。
-- 「一人一箱」的年代那两件事是同一件，发信时拿 sender_id 反查回信箱永远对。
--
-- 放开多信箱之后它错了，而且**不报错**：
--
--   · 你在 Gmail 那个箱里点回复，信从默认的 263 地址发出去，客户看到的
--     发件人不是他刚才写信的那个人
--   · 已发送副本存进默认箱的 SENT 文件夹，你在 Gmail 里翻不到自己刚发的信
--   · 用哪台 SMTP 服务器也跟着默认箱走
--   · 最阴的一条：一封排队中的信重试时才反查信箱，如果这期间换过默认箱，
--     **重试会从另一个地址发出去**——同一封信，两次尝试两个发件人
--
-- 日志里其实一直在喊：「这个人名下有多个信箱，而发信路径还没有账号维度」。
--
-- 0 表示「不知道」。只有两种行会是 0：这次迁移之前入队、还没发出去的那些，
-- 以及回填时找不到对应信箱的历史行。发信那边见到 0 就退回老办法（按人查
-- 默认箱）——和改动之前一模一样，所以部署那一刻队列里积着的信不会卡住。
ALTER TABLE email_messages ADD COLUMN account_id BIGINT NOT NULL DEFAULT 0;

-- 回填分两步，因为两种行知道的事情不一样。
--
-- 一、已经发出去的：from_email 上写着当时真正用的那个地址（发送成功那一刻
-- 盖的章），按地址认回信箱最准。
UPDATE email_messages m
   SET account_id = a.id
  FROM mail_accounts a
 WHERE a.tenant_id = m.tenant_id
   AND lower(a.email) = lower(m.from_email)
   AND m.from_email <> ''
   AND m.account_id = 0;

-- 二、还没发出去的（from_email 还是空的）：填这个人的默认信箱——那正是
-- 改动之前发信时会反查到的那一个。所以回填之后，队列里积着的信从哪个地址
-- 发出去，和不做这次改动完全一样。
--
-- DISTINCT ON 取默认的那一个：ListMailAccountsForEmployee 也是这个顺序，
-- 两处必须同口径，否则回填填的和运行时算的不是一个箱。
UPDATE email_messages m
   SET account_id = d.id
  FROM (
      SELECT DISTINCT ON (tenant_id, employee_id) tenant_id, employee_id, id
        FROM mail_accounts
       ORDER BY tenant_id, employee_id, is_default DESC, id
  ) d
 WHERE d.tenant_id = m.tenant_id
   AND d.employee_id = m.sender_id
   AND m.account_id = 0;

-- 发信 worker 每次都按 (tenant, status, 到点没) 捞一批，不按信箱捞——所以
-- 这一列不进索引。加进去只会让每次写多维护一棵树，换不来任何一次查询。

-- 草稿也要记：「我在 Gmail 里写了一半」这件事，下次打开草稿得还原出来。
-- 不记的话接着写完一发，又从默认箱出去了——而人根本不会想到要去检查发件人。
--
-- 不回填。草稿是没发出去的东西，0 在打开时会退回「按当前在看的箱」，
-- 那正是人重新打开它时的处境。
ALTER TABLE email_drafts ADD COLUMN account_id BIGINT NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE email_drafts DROP COLUMN account_id;
ALTER TABLE email_messages DROP COLUMN account_id;
