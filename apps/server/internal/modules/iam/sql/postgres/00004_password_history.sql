-- +goose Up
CREATE TABLE iam_password_history (
    id BIGINT PRIMARY KEY,
    principal_id BIGINT NOT NULL REFERENCES iam_principals(id) ON DELETE CASCADE,
    password_hash TEXT NOT NULL,
    created_at BIGINT NOT NULL
);
CREATE INDEX idx_iam_password_history_principal ON iam_password_history (principal_id, created_at DESC);

-- +goose Down
DROP TABLE iam_password_history;