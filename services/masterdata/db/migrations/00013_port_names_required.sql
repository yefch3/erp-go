-- +goose Up
-- 中文 ERP 中统一维护中文展示名，同时保留 UN/LOCODE 使用的标准英文名称。
ALTER TABLE ports
  ADD CONSTRAINT ports_name_zh_required CHECK (btrim(name_zh) <> ''),
  ADD CONSTRAINT ports_name_en_required CHECK (btrim(name_en) <> '');

-- +goose Down
ALTER TABLE ports
  DROP CONSTRAINT ports_name_zh_required,
  DROP CONSTRAINT ports_name_en_required;
