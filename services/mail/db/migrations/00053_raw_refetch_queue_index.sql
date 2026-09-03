-- +goose NO TRANSACTION
-- +goose Up

-- 「原件没存下来的信」这条队列的索引。
--
-- 入库时先写对象存储再写行；写对象失败时行照样入库，只是 raw_key 留空
-- （见 inbound.go：一个承诺了 raw_key 却没写成的行，比没有行更坏）。
-- RunRawOriginalRefetch 每次启动把这些行挑出来，回原邮箱按 Message-ID 把
-- 原件补拉回来。
--
-- 从前它靠全表扫：条件是 raw_key = '' AND message_id <> ''，没有任何索引能
-- 用上。这在一万七千封信的时候是几百毫秒，在十七万封的时候就不是了——而
-- 邮件只会越来越多，重启却不会越来越少。
--
-- **局部索引**，所以它的大小等于「此刻有多少封信的原件没存下来」，正常情况
-- 下是零行、几 KB。不是「每封信一行」的那种索引：那样的话为了一个极少发生
-- 的故障，每一次收信都要多维护一条索引项。
--
-- 同一批改动里删掉了七条一次性的历史修补（见 mail/cmd/main.go）。它们各自
-- 也在每次启动扫全表，但那些是修完就该走的东西，给它们建索引是把临时的
-- 变成永久的。这一条不一样：写对象存储随时可能失败，它得一直在。
CREATE INDEX CONCURRENTLY IF NOT EXISTS email_inbound_missing_raw_idx
    ON email_inbound (tenant_id, id)
    WHERE raw_key = '' AND message_id <> '';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS email_inbound_missing_raw_idx;
