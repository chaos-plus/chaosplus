-- +goose Up
CREATE TABLE agent_specs (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'digital_human', -- digital_human | executor
    runtime TEXT NOT NULL DEFAULT 'claude',
    model TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    system_prompt TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE channels (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    created_at BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE channel_members (
    channel_id TEXT NOT NULL,
    member_id TEXT NOT NULL,
    kind TEXT NOT NULL, -- agent | human
    PRIMARY KEY (channel_id, member_id, kind)
);

CREATE TABLE channel_messages (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT UNIQUE NOT NULL,
    channel_id TEXT NOT NULL,
    ts BIGINT NOT NULL,
    author_member_id TEXT NOT NULL DEFAULT '',
    author_kind TEXT NOT NULL DEFAULT 'human', -- human | agent | workflow
    idempotency_key TEXT UNIQUE NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_channel_messages_channel_seq ON channel_messages(channel_id, seq);

-- +goose Down
DROP TABLE agent_specs;
DROP TABLE channels;
DROP TABLE channel_members;
DROP TABLE channel_messages;
