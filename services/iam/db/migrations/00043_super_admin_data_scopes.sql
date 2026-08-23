-- +goose Up

-- 订正：超管的数据范围一直是空的（2026-08-23 做 E1 时发现）。
--
-- 症状是「管理员打开应收到期清单，一条都没有」，而权限页上一切正常。
-- 根因是引导程序与迁移的时序：
--
--   00005 给 SUPER_ADMIN 配 export=ALL，跑在 01:20:58
--   SUPER_ADMIN 角色由引导程序创建，在 01:29:03（晚八分钟）
--
-- 迁移跑的时候 roles 表还是空的，那句 INSERT ... FROM roles 一行没插；
-- 而引导程序只授了权限（ListPermissions 全给），一个数据范围都没配。
-- 解析器对未配置的模块兜底 SELF，于是超管能打开每一个页面，却在每个
-- 页面上只看得见自己经手的那几张单。
--
-- 采购三个模块看起来正常纯属巧合：它们的种子迁移（00031/00037/00041）
-- 跑在引导之后，那时角色已经存在了。
--
-- 两处一起修：本迁移补已有租户，bootstrap.go 的 superAdminScopeModules
-- 管住以后每一个新租户。
--
-- **mail 不在列内**，而且是有意的：邮件正文是系统里最私密的东西，让超管
-- 看别人的邮箱应当是一个显式的、留下记录的决定，不该由订正迁移默默给出。
--
-- tenant_id 跟着角色自己走（r.tenant_id，不是字面量 1）——同 00042 的教训。
INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type)
SELECT r.tenant_id, r.id, m.module, 'ALL'
FROM roles r
CROSS JOIN (VALUES
    ('export'),
    ('shipping'),
    ('procurement_order'),
    ('procurement_requirement'),
    ('procurement_sourcing')
) AS m(module)
WHERE r.code = 'SUPER_ADMIN'
-- 已经配过的不动：管理员可能已经手工调过某个模块，订正不该覆盖他的决定。
ON CONFLICT (tenant_id, role_id, module) DO NOTHING;

-- +goose Down
-- 只收回本迁移可能补上的那几行，且只针对超管——其余角色的范围与它无关。
DELETE FROM role_data_scopes
WHERE module IN ('export', 'shipping', 'procurement_order',
                 'procurement_requirement', 'procurement_sourcing')
  AND role_id IN (SELECT id FROM roles WHERE code = 'SUPER_ADMIN');
