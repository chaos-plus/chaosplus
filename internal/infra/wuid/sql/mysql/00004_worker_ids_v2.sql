-- +goose Up
DROP TABLE IF EXISTS worker_ids;
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE worker_ids;
