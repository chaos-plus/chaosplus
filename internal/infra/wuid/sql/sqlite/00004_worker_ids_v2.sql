-- +goose Up
DROP TABLE IF EXISTS worker_ids;
CREATE TABLE worker_ids (
    id         INTEGER NOT NULL PRIMARY KEY,
    token      TEXT    NOT NULL,
    expires_at BIGINT  NOT NULL,
    os         TEXT,
    host       TEXT,
    ipv4_lan   TEXT,
    mac        TEXT,
    disk       TEXT,
    container  BOOLEAN,
    kvm        BOOLEAN
);

-- +goose Down
DROP TABLE worker_ids;
