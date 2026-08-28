-- +goose Up

-- 员工头像。存的是对象存储里的 key，不是 URL——URL 带签名、会过期，
-- 而且换个存储端点就全失效；key 是「这张图叫什么」，端点是部署的事。
--
-- 空字符串而不是 NULL：整张表的可选文本字段（english_name、remark）都用
-- 空串，读的地方不用各写一遍 NULL 判断。没有头像和头像是空串是同一件事。
ALTER TABLE employees
  ADD COLUMN avatar_key VARCHAR(255) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE employees DROP COLUMN IF EXISTS avatar_key;
