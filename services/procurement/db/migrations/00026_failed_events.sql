-- +goose Up
-- 放弃处理的事件停在这里，而不是消失。
--
-- 原来的消费循环「先记已处理、再去处理」，处理失败就再也不会跑了——一笔
-- 采购收货、一份生效的合同，一次失败就没了，日志还写着 will retry。队列对
-- 「这条处理不了」只有三种答复：丢掉、永远堵着、或者放到一边让人看见。前两
-- 种都是不小心就会发生的那种，而且都长得像「什么都没发生」。
--
-- 这张表是第三种。见 pkg/deadletter。
CREATE TABLE IF NOT EXISTS failed_events (
    id             BIGSERIAL    PRIMARY KEY,
    tenant_id      BIGINT       NOT NULL DEFAULT 1,
    event_id       VARCHAR(200) NOT NULL,
    consumer_group VARCHAR(100) NOT NULL,
    topic          VARCHAR(200) NOT NULL DEFAULT '',
    event_type     VARCHAR(200) NOT NULL DEFAULT '',
    aggregate_id   VARCHAR(200) NOT NULL DEFAULT '',
    payload        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    reason         TEXT         NOT NULL DEFAULT '',
    parked_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (event_id, consumer_group)
);
CREATE INDEX failed_events_recent_idx ON failed_events (tenant_id, parked_at DESC);

-- +goose Down
DROP TABLE failed_events;
