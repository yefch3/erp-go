-- +goose Up
-- 72 号只和解了 59 号自己：main 的 59 是登录名登录，重建分支的 59 是 D1 给
-- 老板的询盘只读授权，两边各应用其一，72 号把后者幂等地再播了一遍。
--
-- 它漏了 61 和 64。走 main 那条历史的库（生产，以及本地跨 worktree 共用的
-- 迁移影子库）跑到 61、64 时 BOSS 这个角色还不存在——`FROM roles r WHERE
-- r.code='BOSS'` 一行都匹配不到，静默插了 0 行——直到 72 才把 BOSS 建出来，
-- 只带一条 sales:inquiry:read。启动时的 EnsurePresetRoles 只补缺的角色、不动
-- 已存在的角色，所以这个只有一条权限的老板会一直留着。
--
-- 这里把 61 和 64 给 BOSS 的东西幂等地再播一遍，两条历史殊途同归；「同归」的
-- 定义是 presetroles.go 里的那行 BOSS，由预置角色对表测试钉住。数据范围用
-- DO NOTHING 而不是 61/64 的 DO UPDATE：走重建路径的库本来就是 ALL，走 main
-- 路径的库根本没有这两行，两边结果一样，而人手改过的范围不会被这里覆盖回去。
INSERT INTO role_permissions(tenant_id,role_id,permission_id)
SELECT r.tenant_id,r.id,p.id FROM roles r CROSS JOIN permissions p
WHERE r.code='BOSS'
  AND p.code IN ('export:quotation:read','export:contract:read','export:contract:approve','approval:task:act','procurement:order:read')
ON CONFLICT DO NOTHING;
INSERT INTO role_data_scopes(tenant_id,role_id,module,scope_type)
SELECT tenant_id,id,'export','ALL' FROM roles WHERE code='BOSS'
ON CONFLICT(tenant_id,role_id,module) DO NOTHING;
INSERT INTO role_data_scopes(tenant_id,role_id,module,scope_type)
SELECT tenant_id,id,'procurement_order','ALL' FROM roles WHERE code='BOSS'
ON CONFLICT(tenant_id,role_id,module) DO NOTHING;
-- +goose Down
-- 和 72 号一样：这些授权可能早于本次和解就存在，回滚不动它们。
SELECT 1;
