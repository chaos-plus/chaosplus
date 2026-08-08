-- +goose Up
CREATE TABLE events (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    instance_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    ts BIGINT NOT NULL,
    type TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    schema_version INTEGER NOT NULL DEFAULT 1,
    payload_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_events_run_seq ON events(run_id, seq);
CREATE INDEX idx_events_instance_seq ON events(instance_id, seq);

-- +goose Down
DROP TABLE events;
