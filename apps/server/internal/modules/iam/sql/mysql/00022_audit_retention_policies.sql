-- +goose Up
CREATE TABLE iam_audit_retention_policies (
    tenant_id BIGINT NOT NULL PRIMARY KEY,
    min_days INT NOT NULL DEFAULT 365,
    archive_after_days INT NOT NULL DEFAULT 730,
    updated_at BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE iam_audit_retention_policies;