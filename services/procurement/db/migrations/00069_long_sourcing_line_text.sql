-- +goose Up
-- 询盘模板允许自由文本；邮件/Excel 里的包装、标准、表面要求等内容经常
-- 超过旧表按界面短输入框设定的 100/200 字符。完整询盘已保存在 JSON，
-- 明细投影也必须无损，否则保存会在任意一条长规格处整体回滚。
-- migration-safety: ALTER COLUMN sourcing_lines text fields TYPE TEXT — 仅放宽 PostgreSQL 字符串列；旧版本仍按 string 读写，与 varchar 完全兼容。
ALTER TABLE sourcing_lines
  ALTER COLUMN product TYPE TEXT,
  ALTER COLUMN material_standard TYPE TEXT,
  ALTER COLUMN grade TYPE TEXT,
  ALTER COLUMN thickness TYPE TEXT,
  ALTER COLUMN width TYPE TEXT,
  ALTER COLUMN length_or_form TYPE TEXT,
  ALTER COLUMN surface_requirement TYPE TEXT,
  ALTER COLUMN coating TYPE TEXT,
  ALTER COLUMN tolerance TYPE TEXT,
  ALTER COLUMN coil_weight TYPE TEXT,
  ALTER COLUMN coil_id TYPE TEXT,
  ALTER COLUMN packaging TYPE TEXT,
  ALTER COLUMN delivery TYPE TEXT,
  ALTER COLUMN payment_terms TYPE TEXT,
  ALTER COLUMN incoterm TYPE TEXT,
  ALTER COLUMN port TYPE TEXT,
  ALTER COLUMN quantity_unit TYPE TEXT;

-- +goose Down
ALTER TABLE sourcing_lines
  ALTER COLUMN product TYPE VARCHAR(200),
  ALTER COLUMN material_standard TYPE VARCHAR(200),
  ALTER COLUMN grade TYPE VARCHAR(120),
  ALTER COLUMN thickness TYPE VARCHAR(100),
  ALTER COLUMN width TYPE VARCHAR(100),
  ALTER COLUMN length_or_form TYPE VARCHAR(120),
  ALTER COLUMN surface_requirement TYPE VARCHAR(200),
  ALTER COLUMN coating TYPE VARCHAR(150),
  ALTER COLUMN tolerance TYPE VARCHAR(150),
  ALTER COLUMN coil_weight TYPE VARCHAR(120),
  ALTER COLUMN coil_id TYPE VARCHAR(120),
  ALTER COLUMN packaging TYPE VARCHAR(200),
  ALTER COLUMN delivery TYPE VARCHAR(200),
  ALTER COLUMN payment_terms TYPE VARCHAR(200),
  ALTER COLUMN incoterm TYPE VARCHAR(80),
  ALTER COLUMN port TYPE VARCHAR(150),
  ALTER COLUMN quantity_unit TYPE VARCHAR(50);
