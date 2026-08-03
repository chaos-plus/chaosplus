-- +goose Up
CREATE TABLE iam_platform_administrators (
    principal_id VARCHAR(64) PRIMARY KEY,
    created_at BIGINT NOT NULL
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE iam_platform_administrators;
