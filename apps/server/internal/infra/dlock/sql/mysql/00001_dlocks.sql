-- +goose Up
CREATE TABLE dlocks (
    name       VARCHAR(191) NOT NULL PRIMARY KEY,
    owner      VARCHAR(255) NOT NULL,
    expires_at BIGINT       NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE dlocks;
