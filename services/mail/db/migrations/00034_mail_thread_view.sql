-- +goose Up

-- One row per conversation per view, so drawing a mailbox page reads the rows
-- it shows instead of every message the person owns.
--
-- The list has always been per-conversation - one line saying "客户回了三次
-- (3)" rather than three lines - and it was assembled at read time: pull every
-- visible message, sort by conversation, run window functions to count them
-- and decide whether any is unread, then take the newest twenty-five. The
-- LIMIT sits at the end of a pipeline whose first step is "read everything",
-- and no index can move it earlier, because a conversation's count and unread
-- badge are facts about all of its messages.
--
-- Measured on a mailbox of 500k messages with one owner holding 50k:
--
--     WindowAgg -> Sort (external merge, Disk: 3496kB) -> 40,877 rows
--     Execution Time: 70 ms
--
-- 70 ms of sorting - spilling to disk - for twenty-five rows, on every page
-- load, per person. It degrades linearly with the mailbox and there is no
-- index that helps.
--
-- The shape below is (thread, view) rather than one wide row per thread with a
-- column set per view. A message belongs to exactly one of INBOX, ARCHIVE,
-- TRASH and JUNK, plus STARRED as an overlay, and giving each its own row
-- means one index serves every view, a new view costs no schema change, and
-- the list query has no CASE in it at all.
CREATE TABLE mail_thread_view (
    tenant_id  BIGINT      NOT NULL,
    owner_id   BIGINT      NOT NULL,
    -- The conversation's identity: thread_key when the headers gave one, else
    -- 'm:<id>' so a message nobody replied to is its own conversation rather
    -- than being merged with every other loose message.
    group_key  VARCHAR(255) NOT NULL,
    -- INBOX | ARCHIVE | TRASH | JUNK | STARRED
    view       VARCHAR(16) NOT NULL,

    msg_count  INTEGER     NOT NULL,
    -- The message that speaks for the conversation in this view: newest first,
    -- id breaking ties. Subject, sender and snippet are read from it.
    last_id    BIGINT      NOT NULL,
    last_at    TIMESTAMPTZ NOT NULL,
    -- Facts about the whole conversation within this view. Unread if ANY
    -- message is unread: a thread with an unanswered question in it must not
    -- look handled because the last line happened to be read.
    any_unread     BOOLEAN NOT NULL,
    any_starred    BOOLEAN NOT NULL,
    any_attachment BOOLEAN NOT NULL,

    PRIMARY KEY (tenant_id, owner_id, group_key, view)
);

-- The only index the list needs, and it serves all five views because the view
-- is a column rather than a set of columns. Keyset pagination reads it in
-- order and stops at twenty-five.
CREATE INDEX mail_thread_view_list_idx
    ON mail_thread_view (tenant_id, owner_id, view, last_at DESC, last_id DESC);

-- +goose StatementBegin
-- Which view a message belongs to, or NULL when it belongs to none.
--
-- This is the one place the rule lives. It used to be a pair of CASE
-- expressions repeated in every query that listed a mailbox, and two copies of
-- a rule are two rules as soon as somebody edits one.
--
-- SENT is deliberately absent: sent mail has its own list, which joins the
-- delivery record for status and tracking and is not a conversation view.
CREATE FUNCTION mail_view_of(
    p_folder TEXT, p_not_junk BOOLEAN, p_is_bounce BOOLEAN,
    p_archived TIMESTAMPTZ, p_deleted TIMESTAMPTZ
) RETURNS TEXT
LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE
        -- A bounce is machinery, not correspondence; it is surfaced through
        -- the needs-attention queue instead.
        WHEN p_is_bounce THEN NULL
        WHEN p_folder NOT IN ('INBOX', 'JUNK') THEN NULL
        -- The trash holds mail deleted from anywhere, junk included.
        WHEN p_deleted IS NOT NULL THEN 'TRASH'
        -- Junk is its own place and only rejoins the mailbox once somebody
        -- rescues it.
        WHEN p_folder = 'JUNK' AND NOT p_not_junk THEN 'JUNK'
        WHEN p_archived IS NOT NULL THEN 'ARCHIVE'
        ELSE 'INBOX'
    END;
$$;
-- +goose StatementEnd

-- What the refresh below looks a conversation up by.
--
-- Without it the trigger is quadratic in the worst way: every refresh scans
-- every message the owner has, so marking one message read costs a pass over
-- a 50k-message mailbox. Found by loading half a million messages and watching
-- the insert fail to finish - the index turns each refresh from a scan of the
-- mailbox into a lookup of the conversation, which is single digits of rows.
--
-- The expression is repeated rather than stored because a generated column
-- cannot reference the row's own id in this Postgres version, and denormalising
-- it into a real column would need its own trigger to stay true - a second
-- thing to keep in step, to avoid maintaining the first.
CREATE INDEX email_inbound_thread_group_idx ON email_inbound
    (tenant_id, owner_id, (coalesce(nullif(thread_key, ''), 'm:' || id::text)));

-- +goose StatementBegin
-- Recomputes one conversation's rows from the messages themselves.
--
-- Recomputed rather than incremented, and that is the whole reliability
-- argument. Counters that are nudged up and down drift: every mutation path
-- has to adjust exactly the right ones, a missed path is invisible until
-- somebody notices a (3) on a two-message thread, and merging two
-- conversations means reconciling two sets of counters. A conversation holds
-- single-digit messages in almost every case, so reading them and writing the
-- answer costs less than the bookkeeping would - and it cannot drift, because
-- there is no state to drift from.
CREATE FUNCTION mail_thread_view_refresh(p_tenant BIGINT, p_owner BIGINT, p_group TEXT)
RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM mail_thread_view
    WHERE tenant_id = p_tenant AND owner_id = p_owner AND group_key = p_group;

    INSERT INTO mail_thread_view (
        tenant_id, owner_id, group_key, view,
        msg_count, last_id, last_at, any_unread, any_starred, any_attachment)
    SELECT p_tenant, p_owner, p_group, v.view,
           count(*)::int,
           (array_agg(v.id ORDER BY v.at DESC, v.id DESC))[1],
           max(v.at),
           bool_or(NOT v.is_read),
           bool_or(v.is_starred),
           bool_or(v.has_attachments)
    FROM (
        SELECT m.id, m.is_read, m.is_starred, m.has_attachments,
               m.received_at AS at,
               mail_view_of(m.folder, m.not_junk, m.is_bounce,
                            m.archived_at, m.deleted_at) AS view
        FROM email_inbound m
        WHERE m.tenant_id = p_tenant AND m.owner_id = p_owner
          AND coalesce(nullif(m.thread_key, ''), 'm:' || m.id::text) = p_group

        UNION ALL

        -- Starred is an overlay, not a place: a starred message is still in
        -- its inbox or its archive, and appears in both lists. Junk that
        -- nobody rescued is excluded, matching the list it replaces - a star
        -- on spam should not pull it back into view.
        SELECT m.id, m.is_read, m.is_starred, m.has_attachments,
               m.received_at, 'STARRED'
        FROM email_inbound m
        WHERE m.tenant_id = p_tenant AND m.owner_id = p_owner
          AND coalesce(nullif(m.thread_key, ''), 'm:' || m.id::text) = p_group
          AND m.is_starred
          AND mail_view_of(m.folder, m.not_junk, m.is_bounce,
                           m.archived_at, m.deleted_at) IN ('INBOX', 'ARCHIVE')
    ) v
    WHERE v.view IS NOT NULL
    GROUP BY v.view;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Keeps the rows true after any change to the messages.
--
-- A trigger rather than calls from the service, because there are thirteen
-- queries that move a message between views - read, starred, archived,
-- deleted, junk-rescued, host mirroring, bulk mark-read, purge, repoint - and
-- a fourteenth will be written by somebody who has never read this file. One
-- place that cannot be bypassed is worth the hidden control flow; thirteen
-- that must each remember is not a design, it is a list of future bugs.
--
-- Statement level with transition tables, so a bulk update that touches two
-- hundred messages refreshes each affected conversation once rather than two
-- hundred times.
CREATE FUNCTION mail_thread_view_sync() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN
        -- Both sides of the change. A message whose thread_key was repointed
        -- leaves one conversation and joins another, and both have to be told.
        SELECT DISTINCT tenant_id, owner_id, group_key FROM (
            SELECT tenant_id, owner_id,
                   coalesce(nullif(thread_key, ''), 'm:' || id::text) AS group_key
            FROM affected_rows
        ) g
    LOOP
        PERFORM mail_thread_view_refresh(r.tenant_id, r.owner_id, r.group_key);
    END LOOP;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER mail_thread_view_insert
    AFTER INSERT ON email_inbound
    REFERENCING NEW TABLE AS affected_rows
    FOR EACH STATEMENT EXECUTE FUNCTION mail_thread_view_sync();

CREATE TRIGGER mail_thread_view_delete
    AFTER DELETE ON email_inbound
    REFERENCING OLD TABLE AS affected_rows
    FOR EACH STATEMENT EXECUTE FUNCTION mail_thread_view_sync();

-- +goose StatementBegin
-- Update needs both tables: the row's old conversation and its new one differ
-- whenever thread_key changes, and only refreshing one leaves the other
-- claiming a message it no longer has.
CREATE FUNCTION mail_thread_view_sync_update() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN
        SELECT DISTINCT tenant_id, owner_id, group_key FROM (
            SELECT tenant_id, owner_id,
                   coalesce(nullif(thread_key, ''), 'm:' || id::text) AS group_key
            FROM old_rows
            UNION ALL
            SELECT tenant_id, owner_id,
                   coalesce(nullif(thread_key, ''), 'm:' || id::text)
            FROM new_rows
        ) g
    LOOP
        PERFORM mail_thread_view_refresh(r.tenant_id, r.owner_id, r.group_key);
    END LOOP;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER mail_thread_view_update
    AFTER UPDATE ON email_inbound
    REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows
    FOR EACH STATEMENT EXECUTE FUNCTION mail_thread_view_sync_update();

-- The mail already stored. Built with the same functions the trigger uses, so
-- history and new mail cannot disagree about what a conversation is.
INSERT INTO mail_thread_view (
    tenant_id, owner_id, group_key, view,
    msg_count, last_id, last_at, any_unread, any_starred, any_attachment)
SELECT tenant_id, owner_id, group_key, view,
       count(*)::int,
       (array_agg(id ORDER BY at DESC, id DESC))[1],
       max(at),
       bool_or(NOT is_read), bool_or(is_starred), bool_or(has_attachments)
FROM (
    SELECT tenant_id, owner_id,
           coalesce(nullif(thread_key, ''), 'm:' || id::text) AS group_key,
           mail_view_of(folder, not_junk, is_bounce, archived_at, deleted_at) AS view,
           id, received_at AS at, is_read, is_starred, has_attachments
    FROM email_inbound
    UNION ALL
    SELECT tenant_id, owner_id,
           coalesce(nullif(thread_key, ''), 'm:' || id::text),
           'STARRED',
           id, received_at, is_read, is_starred, has_attachments
    FROM email_inbound
    WHERE is_starred
      AND mail_view_of(folder, not_junk, is_bounce, archived_at, deleted_at)
          IN ('INBOX', 'ARCHIVE')
) v
WHERE view IS NOT NULL
GROUP BY tenant_id, owner_id, group_key, view;

-- +goose Down
DROP TRIGGER mail_thread_view_update ON email_inbound;
DROP TRIGGER mail_thread_view_delete ON email_inbound;
DROP TRIGGER mail_thread_view_insert ON email_inbound;
DROP FUNCTION mail_thread_view_sync_update();
DROP FUNCTION mail_thread_view_sync();
DROP FUNCTION mail_thread_view_refresh(BIGINT, BIGINT, TEXT);
DROP TABLE mail_thread_view;
DROP INDEX email_inbound_thread_group_idx;
DROP FUNCTION mail_view_of(TEXT, BOOLEAN, BOOLEAN, TIMESTAMPTZ, TIMESTAMPTZ);
