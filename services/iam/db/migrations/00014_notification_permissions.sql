-- +goose Up

-- Correspondence gets its own permission set. Writing to a customer is an act
-- the company is answerable for, and reading somebody else's correspondence is
-- a different act again — hence a read/write pair plus a separate one for the
-- suppression list, which is neither.
INSERT INTO permissions (code, name, module, menu_path) VALUES
  ('notification:email:read',        '查看邮件往来', 'notification', '/notification/emails'),
  ('notification:email:write',       '撰写与群发邮件', 'notification', ''),
  -- Lifting an address off the suppression list re-enables sending to a
  -- mailbox that bounced or complained. Get that wrong across enough
  -- addresses and the sending domain's reputation goes, which silently
  -- degrades delivery for every other mail the company sends. It is an
  -- administrative act, not a sales one.
  ('notification:suppression:write', '维护拒收名单', 'notification', '')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN'
  AND p.module = 'notification'
ON CONFLICT DO NOTHING;

-- Sales sends the mail. Both sales roles get read and write: writing to
-- customers is the job, not an extra privilege. Neither gets the suppression
-- list — lifting a complained address off it re-enables sending to somebody
-- who asked to be left alone, and that decision belongs above the desk that
-- wants the reply.
INSERT INTO role_permissions (tenant_id, role_id, permission_id)
SELECT 1, r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('SALES_MANAGER', 'SALES_RO')
  AND p.code IN ('notification:email:read', 'notification:email:write')
ON CONFLICT DO NOTHING;

-- Data scope for correspondence. The default is SELF: a salesperson sees the
-- mail they sent and nobody else's. A manager with ALL sees the team's, which
-- is the "老板能看见员工发的邮件" requirement — expressed as configuration on
-- the org chart rather than as a special case in the mail code.
--
-- Seeded for every existing role explicitly. Without a row the resolver falls
-- back to SELF anyway, but leaving it implicit means the roles page shows a
-- blank where a real setting should be, and an administrator cannot change a
-- setting they cannot see.
INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT 1, id, 'notification', CASE code
    WHEN 'SUPER_ADMIN'   THEN 'ALL'
    WHEN 'SALES_MANAGER' THEN 'ALL'
    ELSE 'SELF'
  END
FROM roles WHERE tenant_id = 1
ON CONFLICT (tenant_id, role_id, module) DO NOTHING;

-- +goose Down
DELETE FROM role_data_scopes WHERE tenant_id = 1 AND module = 'notification';
DELETE FROM role_permissions WHERE permission_id IN (
  SELECT id FROM permissions WHERE module = 'notification');
DELETE FROM permissions WHERE module = 'notification';
