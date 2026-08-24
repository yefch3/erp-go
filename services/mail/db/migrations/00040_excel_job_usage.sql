-- +goose Up

-- 智能转换用了多少 token（计量第一步）。
--
-- 这个功能是目前唯一一处按次花真钱的地方：每转换一封客户来信就调一次模型。
-- 但在此之前，系统只记了用的是哪个模型，没记用了多少——所以「这个月花了
-- 多少」「谁花的」「该给客户多少额度」全都答不出来，任何配额都只能拍脑袋。
--
-- **存 token 数，不存金额。** token 是事实；折成多少钱是判断，会因为谈下
-- 折扣、换模型、汇率而变。把当时算出来的金额存进去，等于把一个会过期的
-- 判断固化成历史。金额在读的时候按当下单价算——同「付款是事实、核销是
-- 判断」（A4）、「订了 100 是事实、认 80 算完是判断」（A5）。
--
-- 累加而不是覆盖：一次任务可能重试几遍，每遍都真花了钱。attempt_count 记
-- 的是试了几次，这两列记的是一共花了多少——重试失败的那几次也算数。
ALTER TABLE mail_excel_jobs
    ADD COLUMN input_tokens  BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    ADD COLUMN output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0);

-- 按租户按月汇总要扫的就是这个范围。jobs 表本身就是账本，不另建计数表：
-- 这套东西一个月的量级是几十到几百次，为它维护一张会和账本失同步的汇总表
-- 是提前优化。真到了扫不动那天，再加物化视图也不迟。
CREATE INDEX mail_excel_jobs_usage_idx
    ON mail_excel_jobs (tenant_id, created_at);

-- +goose Down
DROP INDEX IF EXISTS mail_excel_jobs_usage_idx;
ALTER TABLE mail_excel_jobs
    DROP COLUMN output_tokens,
    DROP COLUMN input_tokens;
