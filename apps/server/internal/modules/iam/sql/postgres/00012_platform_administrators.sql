-- +goose Up
CREATE TABLE iam_platform_administrators (
    principal_id BIGINT PRIMARY KEY,
    created_at BIGINT NOT NULL
);

-- +goose Down
DROP TABLE iam_platform_administrators;