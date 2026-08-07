-- +goose Up
ALTER TABLE worker_ids RENAME TO worker_ids_legacy;
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
INSERT INTO worker_ids (id, token, expires_at, host)
SELECT id, owner, expires_at, host FROM worker_ids_legacy;
DROP TABLE worker_ids_legacy;

-- +goose Down
ALTER TABLE worker_ids RENAME TO worker_ids_legacy;
CREATE TABLE worker_ids (
    id         INTEGER NOT NULL PRIMARY KEY,
    owner      TEXT    NOT NULL,
    host       TEXT    NOT NULL,
    expires_at BIGINT  NOT NULL,
    meta       TEXT
);
INSERT INTO worker_ids (id, owner, host, expires_at, meta)
SELECT id, token, COALESCE(host, ''), expires_at, NULL FROM worker_ids_legacy;
DROP TABLE worker_ids_legacy;
