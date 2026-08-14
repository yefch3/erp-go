-- Invariants that stored mail must satisfy, and what it means when it does not.
--
-- Written because two of the three bugs found on 2026-08-11/12 were found by
-- looking, not by anybody reporting them. Both were silent: the request
-- succeeded, nothing was logged, and the damage was only visible if a person
-- happened to open the one affected message. A mailbox of a few thousand
-- messages hides a 0.3% fault rate perfectly - enough for someone to notice
-- occasionally, never enough for it to look systematic.
--
-- Two severities, and the distinction is the point. A BLOCK is a statement
-- about our own code: no sender can cause it, so a non-zero count is a bug
-- here. A NOTE counts things a sender is entitled to do - mail with no
-- subject, mail addressed only by Bcc - where a number is worth a glance and
-- nothing more. Mixing the two is how a check gets ignored.
--
-- Each row is  severity|name|count|what it means.

\pset tuples_only on
\pset format unaligned
\pset fieldsep '|'

WITH checks AS (

-- ─── BLOCK: only our own code can cause these ────────────────────────────

  -- 2026-08-12. The key was tenant/account/uid and a UID is unique within a
  -- folder, so INBOX/SENT/JUNK messages sharing a number shared an object.
  -- Worse than a lost file: every repair pass re-parses from here, so a shared
  -- original turns "re-read the original" into "write a stranger's mail in".
  SELECT 'BLOCK' AS sev, '多封信共用同一个原件' AS name,
         (SELECT coalesce(sum(n), 0) FROM (
            SELECT count(*) AS n FROM email_inbound
            WHERE raw_key <> '' GROUP BY raw_key HAVING count(*) > 1) t) AS n,
         '原件被互相覆盖。任何重新解析原件的修复都会把别人的邮件写进这一行' AS why

  -- 2026-08-11. Any part with a Content-ID was filed as an attachment, and
  -- LinkedIn puts one on its text/plain and text/html alternatives, so the
  -- body was taken away and the message stored with nothing in it.
  UNION ALL SELECT 'BLOCK', '正文全空但原件还在',
         (SELECT count(*) FROM email_inbound
          WHERE body_html = '' AND body_text = '' AND raw_key <> ''),
         '解析丢了正文。原件还在，可以重新解析找回'

  -- The conversation rows the mailbox list is drawn from, checked against the
  -- messages they summarise. They are maintained by trigger (migration 00034),
  -- so a mismatch means either a mutation path that slipped past it or a bug
  -- in the refresh itself - and the symptom would be a wrong (3) on a thread
  -- or a conversation in the wrong view, which nobody would report as a bug.
  UNION ALL SELECT 'BLOCK', '会话表与邮件不一致',
         (SELECT count(*) FROM (
            (SELECT tenant_id, owner_id, group_key, view, msg_count, last_id,
                    any_unread, any_starred, any_attachment
             FROM mail_thread_view
             EXCEPT
             SELECT tenant_id, owner_id, group_key, view, count(*)::int,
                    (array_agg(id ORDER BY at DESC, id DESC))[1],
                    bool_or(NOT is_read), bool_or(is_starred), bool_or(has_attachments)
             FROM (
               SELECT tenant_id, owner_id,
                      coalesce(nullif(thread_key, ''), 'm:' || id::text) AS group_key,
                      mail_view_of(folder, not_junk, is_bounce, archived_at, deleted_at) AS view,
                      id, received_at AS at, is_read, is_starred, has_attachments
               FROM email_inbound
               UNION ALL
               SELECT tenant_id, owner_id,
                      coalesce(nullif(thread_key, ''), 'm:' || id::text), 'STARRED',
                      id, received_at, is_read, is_starred, has_attachments
               FROM email_inbound
               WHERE is_starred
                 AND mail_view_of(folder, not_junk, is_bounce, archived_at, deleted_at)
                     IN ('INBOX', 'ARCHIVE')
             ) src WHERE view IS NOT NULL
             GROUP BY tenant_id, owner_id, group_key, view)
            UNION ALL
            (SELECT tenant_id, owner_id, group_key, view, count(*)::int,
                    (array_agg(id ORDER BY at DESC, id DESC))[1],
                    bool_or(NOT is_read), bool_or(is_starred), bool_or(has_attachments)
             FROM (
               SELECT tenant_id, owner_id,
                      coalesce(nullif(thread_key, ''), 'm:' || id::text) AS group_key,
                      mail_view_of(folder, not_junk, is_bounce, archived_at, deleted_at) AS view,
                      id, received_at AS at, is_read, is_starred, has_attachments
               FROM email_inbound
               UNION ALL
               SELECT tenant_id, owner_id,
                      coalesce(nullif(thread_key, ''), 'm:' || id::text), 'STARRED',
                      id, received_at, is_read, is_starred, has_attachments
               FROM email_inbound
               WHERE is_starred
                 AND mail_view_of(folder, not_junk, is_bounce, archived_at, deleted_at)
                     IN ('INBOX', 'ARCHIVE')
             ) src WHERE view IS NOT NULL
             GROUP BY tenant_id, owner_id, group_key, view
             EXCEPT
             SELECT tenant_id, owner_id, group_key, view, msg_count, last_id,
                    any_unread, any_starred, any_attachment
             FROM mail_thread_view)
          ) d),
         '列表里的会话数、未读、所属视图可能是错的'

  -- Search reads search_text, not the body. A message whose text never made it
  -- into that column is invisible to search while looking perfectly normal in
  -- the list - the worst shape for a customer hunting an old quotation.
  UNION ALL SELECT 'BLOCK', '有正文却没有搜索文本',
         (SELECT count(*) FROM email_inbound
          WHERE (body_html <> '' OR body_text <> '') AND search_text = ''),
         '这些信搜不到，但在列表里看着正常'

  -- The list shows a paperclip from this flag and the reader lists the rows.
  -- Embedded pictures the body already displays are deliberately not
  -- attachments to a reader (hideEmbedded), so they are excluded here - an
  -- earlier version of this check counted them and reported 8 false positives.
  UNION ALL SELECT 'BLOCK', '附件标记与实际不符',
         (SELECT count(*) FROM email_inbound e
          WHERE NOT e.has_attachments AND EXISTS (
            SELECT 1 FROM email_inbound_attachments a
            WHERE a.inbound_id = e.id
              AND (a.content_id = '' OR e.body_html NOT LIKE '%cid:' || a.content_id || '%'))),
         '列表不显示回形针，打开却有附件'

  UNION ALL SELECT 'BLOCK', '标记有附件却一条记录都没有',
         (SELECT count(*) FROM email_inbound e
          WHERE e.has_attachments
            AND NOT EXISTS (SELECT 1 FROM email_inbound_attachments a WHERE a.inbound_id = e.id)),
         '列表显示回形针，打开是空的'

  -- A row promising bytes that were never written. Downloading it 404s.
  UNION ALL SELECT 'BLOCK', '附件没有存储键或为 0 字节',
         (SELECT count(*) FROM email_inbound_attachments WHERE file_key = '' OR file_size = 0),
         '点下载会失败'

  -- Threading, replies and the sent-mail join all key on Message-ID.
  UNION ALL SELECT 'BLOCK', 'Message-ID 为空',
         (SELECT count(*) FROM email_inbound WHERE message_id = ''),
         '会话归组和回复关联都靠它'

  -- Header decoding that did not run: the reader sees the raw encoded word.
  UNION ALL SELECT 'BLOCK', '标题仍是未解码的 =?...?=',
         (SELECT count(*) FROM email_inbound WHERE subject LIKE '=?%?=%'),
         'MIME 头没解码，界面上是一串乱码'

  -- Charset handling that failed. U+FFFD is what a decoder writes when it gave
  -- up, so its presence means the text is already lossy in storage.
  UNION ALL SELECT 'BLOCK', '正文或标题含替换字符',
         (SELECT count(*) FROM email_inbound
          WHERE subject LIKE '%' || chr(65533) || '%' OR body_text LIKE '%' || chr(65533) || '%'),
         '字符集解码失败，存下来的文本已经损坏'

  -- The Date header can be anything; INTERNALDATE is the host's own clock and
  -- ingest falls back to it. Both being absent or absurd means neither worked.
  UNION ALL SELECT 'BLOCK', '发信时间缺失或明显错误',
         (SELECT count(*) FROM email_inbound
          WHERE sent_at IS NULL OR sent_at < '1995-01-01' OR sent_at > now() + interval '2 days'),
         '排序和会话时间线会错乱'

-- ─── NOTE: a sender is entitled to these; look, do not block ─────────────

  UNION ALL SELECT 'NOTE', '没有原件',
         (SELECT count(*) FROM email_inbound WHERE raw_key = ''),
         '无法重新解析。可能是存储当时失败，也可能是冲突修复清空的'

  UNION ALL SELECT 'NOTE', '正文引用 cid: 但没有对应部件',
         (SELECT count(*) FROM email_inbound e
          WHERE e.body_html LIKE '%cid:%'
            AND NOT EXISTS (SELECT 1 FROM email_inbound_attachments a
                            WHERE a.inbound_id = e.id AND a.content_id <> '')),
         '多半是回复时保留了引用历史里的图片标签却没附图，发件人没给'

  UNION ALL SELECT 'NOTE', '标题为空',
         (SELECT count(*) FROM email_inbound WHERE subject = ''),
         '有人确实会发无标题邮件'

  UNION ALL SELECT 'NOTE', '收件人为空',
         (SELECT count(*) FROM email_inbound WHERE to_email = ''),
         '纯 Bcc 投递和 undisclosed-recipients 都是合法的'

  UNION ALL SELECT 'NOTE', '有正文但没有摘要',
         (SELECT count(*) FROM email_inbound
          WHERE (body_html <> '' OR body_text <> '') AND snippet = ''),
         '正文可能本来就只有一个空标签，没东西可摘'
)
SELECT sev, name, n, why FROM checks ORDER BY (sev = 'BLOCK') DESC, n DESC, name;
