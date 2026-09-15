-- +goose Up
-- 转换出来的 Excel 搬去对象存储，库里只留一个 key。
--
-- file_data 是这个服务唯一一处真的把文件字节写进 PostgreSQL 的地方；别处的
-- 文件（附件、原始邮件、内嵌图片、在线改过的版本）都只存 key。这一列是当初
-- 图省事留下的例外，代价不在于难看：库里的二进制要进每一次备份、每一条 WAL，
-- 每一次 vacuum 都要绕过它，而一个 key 是六十个字节。
--
-- 还有一条更实在的：这份字节今天要走「库 → 邮件服务 → gRPC → 网关 → 浏览器」
-- 整条路，占的是网关那条 gRPC 通道的预算——那个上限已经被附件打包下载逼到
-- 48 MB 了。搬去对象存储之后它可以不经过我们的服务。
--
-- **file_data 不删**，而且不是为了兼容旧行。它换了一个角色：**对象存储写不
-- 进去时的退路**。
--
-- 为什么需要退路：模型那一次调用是花了钱、等了几十秒的，而任务级的重试
-- （ClaimExcelJob 十五分钟后重新领取）会把那次调用一起重跑，也就是再花一次钱。
-- 所以上传失败时正确的做法不是让任务失败，是把手里已经有的字节先存下来——
-- 存在这一列里，人照常从原来的下载路径拿到文件，什么都没丢。
--
-- 这条退路上的字节由 RunExcelPayloadSweeper 按同一个窗口收走，和 file_key
-- 指向的对象一起。
-- ---- 两次写，不是一个事务 ----
--
-- 对象存储和 PostgreSQL 是两套东西，中间没有事务可言。所以这里不追求原子，
-- 追求的是**每一种失败都停在一个安全的状态上**：
--
--   · 对象写失败 → 字节退回 file_data，行说"我是内联的"，人照常拿得到；
--   · 对象写成功、行写失败 → 对象成了孤儿：没有任何一行指着它。
--
-- 第二种是这次要专门堵的。堵法不是加事务（加不了），是**让对象键可以从行
-- 本身算出来**：mail-excel/<租户>/<任务号>.xlsx，不依赖任何存下来的字段。
-- 于是清理的时候，不管这一行的列里写着什么，都能算出"它可能有一个对象"
-- 并去删一次（S3 的 DELETE 对不存在的键是幂等的）。
ALTER TABLE mail_excel_jobs
    ADD COLUMN file_key VARCHAR(512) NOT NULL DEFAULT '',
    -- 这一行的结果收干净了没有。
    --
    -- 不能用"file_key 空且 file_data 空"当收干净的判据——那正是孤儿的样子。
    -- 需要一个独立的标记，清理器才既能扫到孤儿，又不会每一趟都重扫同一批。
    ADD COLUMN payload_cleared_at TIMESTAMPTZ;

COMMENT ON COLUMN mail_excel_jobs.file_key IS
    '转换结果在对象存储里的位置；空表示退回存在 file_data 里（对象存储写失败时）';
COMMENT ON COLUMN mail_excel_jobs.file_data IS
    '仅当 file_key 为空时有值：对象存储写不进去时的退路，见迁移 00066';

-- 清理器按「结束了、还没收过」找行。**不看 file_key / file_data**，理由见
-- 上面那段：孤儿的那两列恰好都是空的。
CREATE INDEX mail_excel_jobs_payload_idx
    ON mail_excel_jobs (completed_at)
    WHERE completed_at IS NOT NULL AND payload_cleared_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS mail_excel_jobs_payload_idx;
ALTER TABLE mail_excel_jobs
    DROP COLUMN IF EXISTS payload_cleared_at,
    DROP COLUMN IF EXISTS file_key;
