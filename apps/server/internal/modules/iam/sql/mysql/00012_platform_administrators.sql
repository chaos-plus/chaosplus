-- +goose Up
CREATE TABLE iam_platform_administrators (
    principal_id BIGINT PRIMARY KEY,
    created_at BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE iam_platform_administrators;