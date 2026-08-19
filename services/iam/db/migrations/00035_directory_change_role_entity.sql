-- 角色的变更也要能记进目录变更日志。
--
-- 建表时（00024）这张表只服务于部门和员工，CHECK 就写死了这两个值。后来
-- 角色权限（SET_PERMISSIONS）和角色数据范围（SET_DATA_SCOPE）也开始往这里
-- 写审计，entity_type 传的是 'ROLE' —— 于是每一次保存角色权限都会撞上
-- CHECK 约束。
--
-- 后果比"报错"更重：写审计和改权限在同一个事务里，约束一挡，整个事务回滚，
-- 权限**根本没保存**。表现是前端 500，而管理员以为只是"网络问题"，重试还是
-- 500，权限却始终改不动。
--
-- 放宽约束而不是去掉它：这个字段是给查询按实体类型过滤用的，写错值应当当场
-- 失败，而不是留一行没人看得懂的日志。
--
-- migration-safety: 只放宽 CHECK 的取值范围，不改列类型、不删数据。旧版本代码
-- 只会写 DEPARTMENT/EMPLOYEE，在新约束下同样合法，因此回滚到上一个版本仍可运行。

-- +goose Up
ALTER TABLE directory_change_logs
  DROP CONSTRAINT directory_change_logs_entity_type_check;

ALTER TABLE directory_change_logs
  ADD CONSTRAINT directory_change_logs_entity_type_check
  CHECK (entity_type IN ('DEPARTMENT', 'EMPLOYEE', 'ROLE'));

-- +goose Down
-- 回滚前先清掉 ROLE 行，否则旧约束加不上（它们是新约束才允许的数据）。
DELETE FROM directory_change_logs WHERE entity_type = 'ROLE';

ALTER TABLE directory_change_logs
  DROP CONSTRAINT directory_change_logs_entity_type_check;

ALTER TABLE directory_change_logs
  ADD CONSTRAINT directory_change_logs_entity_type_check
  CHECK (entity_type IN ('DEPARTMENT', 'EMPLOYEE'));
