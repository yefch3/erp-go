-- +goose Up

-- Taking a conversation out of the system is its own act.
--
-- Reading a customer's correspondence and walking out of the building with it
-- are not the same thing, even though the same person does both with the same
-- eyes. Until now they were the same permission, which meant there was no
-- sentence an administrator could write that said "you may keep answering
-- your mail, but you may not package it up any more".
--
-- Note what this does *not* claim to do. Anyone who can read a mail can
-- select it, copy it and paste it somewhere else; no permission stops that,
-- and pretending otherwise would be theatre. What the pair below buys is
-- narrower and real: one named capability that can be withdrawn from one
-- person without taking away their inbox, and a record with a name in it
-- every time it is used.
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('mail:email:export', '导出邮件会话', 'mail', ''),
  -- Reading the record is deliberately not the same permission as making
  -- entries in it. Someone who can quietly edit what the log says about their
  -- own exports has an audit trail in name only, and the first step towards
  -- that is being the only person who ever looks at it.
  ('mail:export:audit', '查看导出记录', 'mail', '/mail/export-log')
ON CONFLICT (code) DO NOTHING;

-- Everyone who can read mail today can export it today.
--
-- Deliberate: the salesperson packaging six months of a negotiation for their
-- manager or for the customs broker *is* the use case, and a gate that made
-- them ask an administrator to do it for them would be worked around within
-- the week — by forwarding the thread to a personal address, which is the
-- same act with no record of it. The permission exists to be taken away from
-- one person, not to be absent from everybody.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('SUPER_ADMIN', 'SALES_MANAGER', 'SALES_RO')
  AND p.code = 'mail:email:export'
ON CONFLICT DO NOTHING;

-- The record is management's, not the desk's. A salesperson who could read
-- the export log would learn exactly how closely they are being watched,
-- which is the one reader it is not for. Scoping still applies on top: a
-- sales manager sees their own team's exports, because the log read runs
-- through the same mail data scope as the team mail view.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('SUPER_ADMIN', 'SALES_MANAGER')
  AND p.code = 'mail:export:audit'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (
  SELECT id FROM permissions WHERE code IN ('mail:email:export', 'mail:export:audit'));
DELETE FROM permissions WHERE code IN ('mail:email:export', 'mail:export:audit');
