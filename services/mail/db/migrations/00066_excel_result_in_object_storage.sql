-- +goose Up
-- 转换出来的 Excel 搬去对象存储，库里只留一个 key。
--
-- file_data 是这个服务唯一一处真的把文件字节长期留在 PostgreSQL 的地方；别处
-- 的文件（附件、原始邮件、内嵌图片、在线改过的版本）都只存 key。代价不在于
-- 难看：库里的二进制要进每一次备份、每一条 WAL，每一次 vacuum 都要绕过它，
-- 而一个 key 是六十个字节。还有一条更实在的——这份字节今天要走"库 → 邮件
-- 服务 → gRPC → 网关 → 浏览器"整条路，占的是网关那条 gRPC 通道的预算，而那
-- 个上限已经被附件打包下载逼到 48 MB 了。
--
-- ---- 落库和写对象存储，是两次写，中间没有事务 ----
--
-- 这件事没法回避：PostgreSQL 和对象存储是两套系统。回避不了，就不能把"两边
-- 都成了"当成正常路径来写代码——总有一次会只成一半。
--
-- 所以顺序反过来：**先只写库，而且一次事务写完；对象存储交给一个会重试到
-- 成功的搬运工。**
--
--   完成任务时：状态、workbook、字节，一条 UPDATE 落地。要么全成，要么全不
--               成——任务不会出现"标着完成、文件却取不到"的状态。
--   搬运工：    把 file_data 传上去，成功之后一条 UPDATE 同时写上 file_key、
--               清掉 file_data。这一条也是原子的。
--   读的时候：  file_key 有就读对象，没有就读 file_data。**任何时刻至少有
--               一处是全的**，所以人永远取得到。
--
-- 这样四种失败分别停在哪，全都是安全的：
--
--   · 落库失败      → 任务没完成，十五分钟后重跑（这是既有行为）。
--   · 传对象失败    → 字节还在库里，人照常拿得到；搬运工退避后再试。
--   · 传成功、清行失败 → 对象在，字节也在，读的仍然是字节。下一趟重传同一个
--                     键（键 = 租户 + 任务号，确定的，覆盖而不是堆积）再清一次。
--   · 一直传不上去  → 字节就一直留在库里。这是**降级，不是丢失**：最坏的
--                     结果是这一份没搬成，而不是人拿不到文件。
--
-- 没有"孤儿对象"这种状态：对象只由搬运工创建，而搬运工只为已经存在的行干活。
ALTER TABLE mail_excel_jobs
    -- 结果在对象存储里的位置。空 = 还在 file_data 里（还没搬，或者搬不动）。
    ADD COLUMN file_key VARCHAR(512) NOT NULL DEFAULT '',
    -- 搬运工的重试状态，和 mail_flag_ops 一套写法。
    ADD COLUMN upload_attempts INT NOT NULL DEFAULT 0,
    ADD COLUMN upload_next_try_at TIMESTAMPTZ,
    ADD COLUMN upload_last_error TEXT NOT NULL DEFAULT '',
    -- 结果收干净了没有（过了留存期）。
    --
    -- 需要独立的标记而不是"两列都空"：那个判据分不出"收过了"和"从来没成功
    -- 过"，而后者不该被当成收完了。
    ADD COLUMN payload_cleared_at TIMESTAMPTZ;

COMMENT ON COLUMN mail_excel_jobs.file_key IS
    '结果在对象存储里的位置；空表示字节还在 file_data 里（搬运工还没搬成）';
COMMENT ON COLUMN mail_excel_jobs.file_data IS
    '结果的字节。落库时先写这里，搬运工传上对象存储之后清空，见迁移 00066';

-- 搬运工要找的：还带着字节、还没有 key、到点可以再试的。
CREATE INDEX mail_excel_jobs_upload_idx
    ON mail_excel_jobs (upload_next_try_at, id)
    WHERE file_data IS NOT NULL AND file_key = '';

-- 留存期清理要找的：结束了、还没收过的。
CREATE INDEX mail_excel_jobs_payload_idx
    ON mail_excel_jobs (completed_at)
    WHERE completed_at IS NOT NULL AND payload_cleared_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS mail_excel_jobs_payload_idx;
DROP INDEX IF EXISTS mail_excel_jobs_upload_idx;
ALTER TABLE mail_excel_jobs
    DROP COLUMN IF EXISTS payload_cleared_at,
    DROP COLUMN IF EXISTS upload_last_error,
    DROP COLUMN IF EXISTS upload_next_try_at,
    DROP COLUMN IF EXISTS upload_attempts,
    DROP COLUMN IF EXISTS file_key;
