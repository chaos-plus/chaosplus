-- +goose Up
CREATE TABLE iam_audit_retention_policies (
    tenant_id VARCHAR(128) NOT NULL PRIMARY KEY,
    min_days INTEGER NOT NULL DEFAULT 365,
    archive_after_days INTEGER NOT NULL DEFAULT 730,
    updated_at BIGINT NOT NULL
);

-- +goose Down
DROP TABLE iam_audit_retention_policies;
