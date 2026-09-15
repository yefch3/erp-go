-- +goose Up
-- 转换出来的 Excel：文件本身进对象存储，库里只留 key 和 metadata。
--
-- file_data 是这个服务唯一一处真的把文件本身长期留在 PostgreSQL 的地方；别处
-- 的文件（附件、原始邮件、内嵌图片、在线改过的版本）都只存 key。库里的二进制
-- 要进每一次备份、每一条 WAL，每一次 vacuum 都要绕过它，而一个 key 是六十个
-- 字节。
--
-- ---- 落库和写对象存储是两次写，中间没有事务：顺序定死 ----
--
--   1. 文件本身 → 对象存储（mail-excel/<租户>/<任务号>.xlsx）
--   2. 一小份 metadata → 对象存储（同名 .json）
--   3. 状态 + file_key + metadata → 这张表，一条 UPDATE
--
-- 文件本身**从不进库**。第 1、2 步写不进去，任务标失败、说清原因，由人决定
-- 什么时候再转；第 3 步写不进去，任务留在「处理中」，下一次领走时从对象
-- 存储里恢复 metadata 直接收尾——**模型不再跑**。细节见 app.processExcelJob。
--
-- 2026-09-15 定的。之前两版都把文件本身写进过库（一版写完再搬，一版写不进
-- 对象存储就退回库里），产品负责人明确不要。
ALTER TABLE mail_excel_jobs
    -- 文件本身在对象存储里的位置。空 = 还没完成，或者已经被清理器收走。
    ADD COLUMN file_key VARCHAR(512) NOT NULL DEFAULT '',
    -- 结果收干净了没有（过了留存期）。
    --
    -- 需要独立的标记而不是"列都空"：那个判据分不出"收过了"和"从来没成功
    -- 过"，而后者不该被当成收完了。
    ADD COLUMN payload_cleared_at TIMESTAMPTZ;

COMMENT ON COLUMN mail_excel_jobs.file_key IS
    '文件本身在对象存储里的位置；空表示还没完成，或者已经被清理器收走';
COMMENT ON COLUMN mail_excel_jobs.file_data IS
    '旧列。2026-09-15 起不再写；改动前完成的任务的文件本身还在这里，留存期一到清空。下一版删列';
-- 行数据不进库。文件里已经全有（公式格旁边连算好的值都存着），预览时从
-- 文件里读回来；这里只留文件里**没有**的那几样。改动前完成的旧任务还带着
-- rows / preview_rows，过了留存期和文件一起清。
COMMENT ON COLUMN mail_excel_jobs.workbook_json IS
    '只有 metadata：表名、说明、表头、每列类型和字段标识，不带行。行在 .xlsx 文件里';

-- 留存期清理要找的：结束了、还没收过的。
CREATE INDEX mail_excel_jobs_payload_idx
    ON mail_excel_jobs (completed_at)
    WHERE completed_at IS NOT NULL AND payload_cleared_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS mail_excel_jobs_payload_idx;
ALTER TABLE mail_excel_jobs
    DROP COLUMN IF EXISTS payload_cleared_at,
    DROP COLUMN IF EXISTS file_key;
