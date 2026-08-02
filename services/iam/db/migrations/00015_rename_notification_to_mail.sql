-- +goose Up

-- The service that owns correspondence is called `mail`, not `notification`:
-- it holds nothing but email, and a name that promises notifications invites
-- somebody to file SMS or in-app alerts here later.
--
-- Permission codes are runtime data, not just source. Renaming them in the
-- code alone would leave every existing role pointing at codes nobody checks,
-- which reads as "permission granted" in the roles page and as "denied" at
-- the door. So the rename happens here, in place, keeping the role
-- assignments attached to the same rows.
--
-- 00014 is left as written: it is history, and an applied migration edited
-- after the fact makes a fresh database and an existing one disagree.
UPDATE permissions
SET code = 'mail:' || substring(code from length('notification:') + 1),
    module = 'mail'
WHERE code LIKE 'notification:%';

-- Menu paths follow the code so the roles page groups them under the new
-- module rather than showing an empty section.
UPDATE permissions
SET menu_path = '/mail/emails'
WHERE code = 'mail:email:read';

-- Data scope is keyed by module name, and the mail service now asks for
-- 'mail'. Without this, every role would silently fall back to SELF and a
-- manager configured to see the team's correspondence would stop seeing it.
UPDATE role_data_scopes
SET module = 'mail'
WHERE module = 'notification';

-- +goose Down
UPDATE role_data_scopes SET module = 'notification' WHERE module = 'mail';
UPDATE permissions
SET code = 'notification:' || substring(code from length('mail:') + 1),
    module = 'notification',
    menu_path = CASE WHEN code = 'mail:email:read' THEN '/notification/emails' ELSE menu_path END
WHERE code LIKE 'mail:%';
