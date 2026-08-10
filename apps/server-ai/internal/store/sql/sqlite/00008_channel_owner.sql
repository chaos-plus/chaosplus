-- +goose Up
-- 频道归属:只有 owner 能解散频道。存量频道的创建者就是本地用户 human。
ALTER TABLE channels ADD COLUMN owner_id TEXT NOT NULL DEFAULT 'human';

-- +goose Down
ALTER TABLE channels DROP COLUMN owner_id;
