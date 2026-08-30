-- +goose Up

-- 供应商对账页上的凭证。
--
-- 需求原话：「供应商发票只是员工用来上传留凭证用的」。发票页下线之后，
-- 「留凭证」这件事得有个新去处，否则删页面就等于把这个能力删了。
--
-- **一张表，不是采购单上加一列**：一张采购单会有好几张纸——发票、水单、
-- 退款回执，分几次到手。一列只能存一份，第二次上传就把第一份顶掉了，
-- 而顶掉的那份恰恰是「上一次是怎么回事」的唯一证据。
--
-- 和银行流水那份对账单的区别也在这：那边一行流水对一张银行出的单子，
-- 一对一是事实；这边一张采购单对多张纸，一对多才是事实。
CREATE TABLE purchase_order_recon_files (
    id               BIGSERIAL     PRIMARY KEY,
    tenant_id        BIGINT        NOT NULL DEFAULT 1,
    po_id            BIGINT        NOT NULL REFERENCES purchase_orders(id),

    -- 对象存储里的 key。前缀固定是 po-recon/{tenant}/{po}/，那道前缀检查
    -- 是所有服务共用一个桶时**唯一**的跨租户隔离，写入时必须校验。
    object_key       VARCHAR(500)  NOT NULL,
    -- 上传时的原始文件名。key 里也能还原，但那要靠切字符串；存一份省事，
    -- 而且将来改 key 的拼法不会让历史记录的显示名跟着乱。
    file_name        VARCHAR(300)  NOT NULL DEFAULT '',
    -- 员工自己写的一句话：这张纸是什么。「8 月发票」「退款回执」。
    note             TEXT          NOT NULL DEFAULT '',

    uploaded_by_id   BIGINT        NOT NULL DEFAULT 0,
    uploaded_by_name VARCHAR(100)  NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT now(),

    -- 撤下来不删行，和这个模块其它地方同一条纪律：「传错了」和「从来没传过」
    -- 是两件事，账上要看得出有人动过。
    removed_at       TIMESTAMPTZ,
    removed_by_id    BIGINT        NOT NULL DEFAULT 0,
    removed_by_name  VARCHAR(100)  NOT NULL DEFAULT ''
);

-- 列表按采购单取还活着的那几份，走这条。
CREATE INDEX po_recon_files_live_idx
    ON purchase_order_recon_files (tenant_id, po_id, id)
    WHERE removed_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS purchase_order_recon_files;
