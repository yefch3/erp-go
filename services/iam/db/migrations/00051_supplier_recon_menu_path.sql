-- +goose Up

-- procurement:recon:read 的 menu_path 还指着 /supplier-statements，那一页
-- 已经下线了。这个码现在真正对应的是 /supplier-recon。
--
-- menu_path 只是权限列表里的陈列数据，前端没有任何地方消费它——指错了不会
-- 白屏。但一份权限清单里挂着三条通往不存在页面的路径，下一个照着它排查
-- 「这个码管的是哪一页」的人会被带到沟里。
UPDATE permissions SET menu_path = '/supplier-recon'
 WHERE code = 'procurement:recon:read';

-- 发票和付款那两组页面也下线了。权限码本身**不删**——删码要动角色授权，
-- 而留着一个没人用的码不会让任何东西出错；procurement:payment:* 更是仍在
-- 用（银行流水那一组路由挂的就是它）。只把指向空地的路径清掉。
UPDATE permissions SET menu_path = ''
 WHERE code IN ('procurement:invoice:read', 'procurement:invoice:write');

-- +goose Down
UPDATE permissions SET menu_path = '/supplier-statements'
 WHERE code = 'procurement:recon:read';
UPDATE permissions SET menu_path = '/supplier-invoices'
 WHERE code = 'procurement:invoice:read';
