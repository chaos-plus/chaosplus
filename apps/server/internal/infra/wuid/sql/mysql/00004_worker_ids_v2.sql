-- +goose Up
ALTER TABLE worker_ids RENAME TO worker_ids_legacy;
CREATE TABLE worker_ids (
    id         INT          NOT NULL PRIMARY KEY,
    token      VARCHAR(64)  NOT NULL,
    expires_at BIGINT       NOT NULL,
    os         VARCHAR(64),
    host       VARCHAR(255),
    ipv4_lan   VARCHAR(255),
    mac        VARCHAR(255),
    disk       TEXT,
    container  TINYINT(1),
    kvm        TINYINT(1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
INSERT INTO worker_ids (id, token, expires_at, host)
SELECT id, owner, expires_at, host FROM worker_ids_legacy;
DROP TABLE worker_ids_legacy;

-- +goose Down
ALTER TABLE worker_ids RENAME TO worker_ids_legacy;
CREATE TABLE worker_ids (
    id         INT          NOT NULL PRIMARY KEY,
    owner      VARCHAR(255) NOT NULL,
    host       VARCHAR(255) NOT NULL,
    expires_at BIGINT       NOT NULL,
    meta       TEXT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
INSERT INTO worker_ids (id, owner, host, expires_at, meta)
SELECT id, token, COALESCE(host, ''), expires_at, NULL FROM worker_ids_legacy;
DROP TABLE worker_ids_legacy;
