-- +goose Up
CREATE TABLE iam_platform_grants (
    principal_id BIGINT NOT NULL,
    permission_code VARCHAR(128) NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (principal_id, permission_code)
);
CREATE INDEX idx_iam_platform_grants_permission ON iam_platform_grants (permission_code, principal_id);

-- +goose Down
DROP TABLE iam_platform_grants;