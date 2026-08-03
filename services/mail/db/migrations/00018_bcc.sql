-- +goose Up

-- Blind carbon copy.
--
-- The third recipient field, and the only one whose defining property is what
-- the *other* recipients do not see. It is stored exactly like the other two —
-- a row per person with a kind — because the difference lives entirely in how
-- the message is built: a BCC address goes into the SMTP envelope and into no
-- header at all.
--
-- VARCHAR(2) was wide enough for TO and CC and is not wide enough for BCC. A
-- silent truncation here would file every blind copy under "BC" and fail the
-- check constraint, so the column grows first.
ALTER TABLE email_message_recipients
    DROP CONSTRAINT email_message_recipients_kind_check;
ALTER TABLE email_message_recipients
    ALTER COLUMN kind TYPE VARCHAR(3);
ALTER TABLE email_message_recipients
    ADD CONSTRAINT email_message_recipients_kind_check
    CHECK (kind IN ('TO','CC','BCC'));

-- Drafts keep the blind list like the other two. A reply saved half-written
-- and reopened has to come back as the same message it was, and a BCC that
-- quietly vanished between saving and sending would be the worst kind of
-- difference: invisible in the composer and invisible in the sent copy.
ALTER TABLE email_drafts
    ADD COLUMN bcc JSONB NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE email_drafts DROP COLUMN bcc;
DELETE FROM email_message_recipients WHERE kind = 'BCC';
ALTER TABLE email_message_recipients
    DROP CONSTRAINT email_message_recipients_kind_check;
ALTER TABLE email_message_recipients
    ALTER COLUMN kind TYPE VARCHAR(2);
ALTER TABLE email_message_recipients
    ADD CONSTRAINT email_message_recipients_kind_check
    CHECK (kind IN ('TO','CC'));
