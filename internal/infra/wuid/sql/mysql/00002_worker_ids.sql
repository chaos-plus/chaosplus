-- +goose Up
CREATE TABLE worker_ids (
    id         INT          NOT NULL PRIMARY KEY,
    owner      VARCHAR(255) NOT NULL,
    host       VARCHAR(255) NOT NULL,
    expires_at BIGINT       NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE worker_ids;
