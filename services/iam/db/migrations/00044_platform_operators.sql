-- +goose Up

-- 平台操作员：能开客户公司的那几个人。
--
-- 刻意不做成权限。每家新公司的超管在引导时被授予**全部**权限
-- （seedTenant 里 ListPermissions 全量循环），一个「开公司」权限会自动
-- 流进每一位客户管理员手里——客户也能开公司了。所以这个身份必须住在
-- 权限系统之外：一张独立的名单，网关和 iam 各查各的，谁都不给客户发。
--
-- 没有 tenant_id，这是有意的（见 check-tenant-id.sh 的豁免注释）：平台
-- 层站在所有公司之上，给它挂公司号等于把「谁能开公司」交给某一家公司
-- 管理。
CREATE TABLE platform_operators (
    employee_id BIGINT      PRIMARY KEY,
    note        TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 引导管理员入列。员工 1 是空库引导出的第一个人——部署这套系统的操作员，
-- 天然就是平台方。硬编码 1 依赖的正是这个构造：BIGSERIAL 的第一行。
-- 库里还没引导过（比如全新环境先跑迁移）就先不插，引导后由人工补一行。
INSERT INTO platform_operators (employee_id, note)
SELECT 1, '空库引导出的第一位管理员，即平台部署者'
WHERE EXISTS (SELECT 1 FROM employees WHERE id = 1)
ON CONFLICT (employee_id) DO NOTHING;

-- +goose Down
DROP TABLE platform_operators;
