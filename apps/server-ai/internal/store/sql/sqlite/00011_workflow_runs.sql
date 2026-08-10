-- +goose Up
CREATE TABLE IF NOT EXISTS workflow_runs (
    id          TEXT PRIMARY KEY,
    def_json    TEXT NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'running',
    created_at  INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE IF EXISTS workflow_runs;
