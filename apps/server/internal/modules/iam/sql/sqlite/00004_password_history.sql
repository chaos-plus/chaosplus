-- +goose Up
CREATE TABLE iam_password_history (
    id TEXT NOT NULL PRIMARY KEY,
    principal_id TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_password_history_principal ON iam_password_history (principal_id, created_at DESC);

-- +goose Down
DROP TABLE iam_password_history;
