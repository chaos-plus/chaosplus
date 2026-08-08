-- +goose Up
CREATE TABLE machine_runners (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL DEFAULT '',
    address TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'confirmed',
    last_heartbeat_at BIGINT NOT NULL DEFAULT 0,
    token_hash TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE machine_runners;
