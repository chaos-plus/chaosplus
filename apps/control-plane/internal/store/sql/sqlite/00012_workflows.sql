-- +goose Up
CREATE TABLE IF NOT EXISTS workflows (
    id          TEXT NOT NULL,
    version     TEXT NOT NULL DEFAULT '1',
    name        TEXT NOT NULL DEFAULT '',
    def_json    TEXT NOT NULL DEFAULT '{}',
    instance_id TEXT NOT NULL DEFAULT '',
    owner_id    TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (id, version)
);

-- +goose Down
DROP TABLE IF EXISTS workflows;
