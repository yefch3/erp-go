-- +goose Up
-- 工厂已从业务模型中退役。部署时数据库迁移先于容器切换，因此这一版先
-- 清空历史数据并保留空兼容表，确保切换前的旧服务和失败回滚仍能启动。
-- 工厂代码和入口已在本版移除；空表将在下一次发布中物理删除。
DELETE FROM factory_change_logs;
DELETE FROM factory_certificates;
DELETE FROM factory_capabilities;
DELETE FROM factory_owners;
DELETE FROM factory_contacts;
DELETE FROM factories;

DELETE FROM option_items WHERE category = 'FACTORY_OWNER_RESPONSIBILITY';
DELETE FROM number_rules WHERE biz_type = 'FACTORY';

-- +goose Down
-- 工厂历史数据已按产品要求删除，无法由迁移自动恢复；兼容表仍然存在。
