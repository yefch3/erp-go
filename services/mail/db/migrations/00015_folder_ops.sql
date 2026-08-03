-- +goose Up

-- Deletion, restore and archiving move a message between folders on the host,
-- and a move changes its UID. The UID we hold is the one it had where it used
-- to be, so it is useless the moment the move lands — following the message
-- afterwards needs the one identifier that travels with it.
--
-- Hence the Message-ID on the queue row: whoever publishes a restore or a
-- purge searches the destination folder for it rather than guessing a number.
ALTER TABLE mail_flag_ops
    ADD COLUMN message_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE mail_flag_ops DROP COLUMN message_id;
