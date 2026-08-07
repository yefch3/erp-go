-- +goose Up

-- A lockout that ends by itself.
--
-- Until now five wrong passwords set users.status = 'LOCKED', and nothing in
-- the codebase ever set it back. Not a timeout — there was none. Not the
-- administrator's 重置密码, which cleared failed_count and left status alone
-- while returning success, so the screen said the account was fixed and it was
-- not. The only recovery was somebody with a psql prompt.
--
-- That was survivable while the login identifier was a username, which is
-- internal and not published. It stopped being survivable when the identifier
-- became the company email address: 名字@公司域名 is on business cards, in
-- every mail signature, and guessable outright. Five requests to a public
-- endpoint destroyed a named person's account with no way back.
--
-- So the lock moves out of status and into a deadline. Two consequences, and
-- both are the point: it expires without anyone doing anything, and it can no
-- longer be confused with an administrator's decision to disable somebody.
ALTER TABLE users ADD COLUMN locked_until TIMESTAMPTZ;

-- Everyone currently locked is a victim of the above, not the subject of a
-- decision: nothing but the failure counter could produce this state. Letting
-- them back in is the migration, not a side effect of it.
UPDATE users SET status = 'ACTIVE', failed_count = 0 WHERE status = 'LOCKED';

-- And 'LOCKED' stops being a value status can hold. Leaving it in the check
-- constraint would leave the door open for the next writer to reintroduce
-- exactly this, and status now means one thing only: what an administrator
-- decided about this account. How badly today is going belongs in locked_until.
ALTER TABLE users DROP CONSTRAINT users_status_check;
ALTER TABLE users ADD CONSTRAINT users_status_check
    CHECK (status IN ('ACTIVE', 'DISABLED'));

-- +goose Down
ALTER TABLE users DROP CONSTRAINT users_status_check;
ALTER TABLE users ADD CONSTRAINT users_status_check
    CHECK (status IN ('ACTIVE', 'LOCKED', 'DISABLED'));
ALTER TABLE users DROP COLUMN IF EXISTS locked_until;
