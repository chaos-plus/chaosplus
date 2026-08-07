-- +goose Up
CREATE TABLE worker_ids (
    id         INTEGER NOT NULL PRIMARY KEY,
    owner      TEXT    NOT NULL,
    host       TEXT    NOT NULL,
    expires_at BIGINT  NOT NULL
);

-- +goose Down
DROP TABLE worker_ids;
