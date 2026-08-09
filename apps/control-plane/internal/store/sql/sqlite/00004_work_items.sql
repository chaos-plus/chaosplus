-- +goose Up
CREATE TABLE work_items (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL DEFAULT 'task',     -- requirement | task | bug
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open',   -- open | in_progress | review | done
    assignee_agent TEXT NOT NULL DEFAULT '',
    channel_id TEXT NOT NULL DEFAULT '',   -- 关联群聊频道(订阅通知回该频道)
    created_at BIGINT NOT NULL DEFAULT 0,
    updated_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_work_items_channel ON work_items(channel_id);
CREATE INDEX idx_work_items_type_status ON work_items(type, status);

-- +goose Down
DROP TABLE work_items;
