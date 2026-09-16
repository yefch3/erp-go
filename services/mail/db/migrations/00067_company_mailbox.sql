-- +goose Up
-- 主邮箱：公司的邮箱，员工只是暂时拿着用。
--
-- 到今天为止，每个信箱都属于某个人：绑的时候永远绑给登录的那个人，请求里连
-- 「绑给谁」这一项都没有（那是有意的安全约束，见 mailbind.go 文件头）。
--
-- 2026-09-16 加进来的口径：公司给每个员工配一个**主邮箱**（sales01@公司 这种），
--   · 管理员输地址和密码替员工绑，员工自己不能改密码、不能解绑
--   · 员工登录 ERP 就自动开着，不用再输一次密码
--   · 员工离职，管理员收回，分给下一个人；历史邮件跟着邮箱走
--
-- 所以信箱分两类。kind 是「这个箱归谁」，不是「这个箱是哪家服务商的」：
--   PERSONAL  员工自己绑的，今天所有存量都是这种
--   COMPANY   管理员分配的主邮箱
--
-- 一个人**同时只能有一个**主邮箱（下面那个部分唯一索引）。换主邮箱是先收回
-- 旧的再分新的；收回过的（unbound_at 有值）不占名额，所以历史留得下。
ALTER TABLE mail_accounts
    ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'PERSONAL'
        CHECK (kind IN ('PERSONAL', 'COMPANY'));

CREATE UNIQUE INDEX mail_accounts_one_company_mailbox_per_employee
    ON mail_accounts (tenant_id, employee_id)
    WHERE kind = 'COMPANY' AND unbound_at IS NULL;

COMMENT ON COLUMN mail_accounts.kind IS
    'PERSONAL=员工自己绑的；COMPANY=管理员分配的主邮箱，员工不能改密码、不能解绑，登录即开';

-- 留痕表加「谁操作的」。
--
-- 从前 employee_id 既是「谁的箱」也是「谁动的手」——自己绑自己的，两者是一个
-- 人。管理员替员工分配主邮箱之后，这两件事分开了：箱是员工的，手是管理员的。
-- 0 = 自己操作的（存量全是），和这套库里表达「没有」的习惯一致。
ALTER TABLE mail_binding_log
    ADD COLUMN actor_id BIGINT NOT NULL DEFAULT 0;

COMMENT ON COLUMN mail_binding_log.actor_id IS
    '操作人。0=员工自己；非 0=替他操作的管理员（分配/收回主邮箱）';

-- +goose Down
ALTER TABLE mail_binding_log DROP COLUMN IF EXISTS actor_id;
DROP INDEX IF EXISTS mail_accounts_one_company_mailbox_per_employee;
ALTER TABLE mail_accounts DROP COLUMN IF EXISTS kind;
