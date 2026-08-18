-- +goose Up

-- 转换任务携带默认询盘模板的列快照：任务入队后模板再改版，不影响已排队
-- 任务的产出列。空数组表示旧任务，按系统内置 21 列处理。
ALTER TABLE mail_excel_jobs ADD COLUMN template_columns JSONB NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE mail_excel_jobs DROP COLUMN template_columns;
