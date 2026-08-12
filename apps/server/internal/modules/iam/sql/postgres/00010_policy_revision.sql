-- +goose Up
CREATE TABLE iam_policy_revisions (
    tenant_id BIGINT PRIMARY KEY,
    revision BIGINT NOT NULL DEFAULT 0 CHECK (revision >= 0),
    updated_at BIGINT NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS iam_policy_revisions;