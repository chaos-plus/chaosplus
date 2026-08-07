-- +goose Up
CREATE TABLE iam_password_history (
    id VARCHAR(64) PRIMARY KEY,
    principal_id VARCHAR(64) NOT NULL,
    password_hash TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    KEY idx_iam_password_history_principal (principal_id, created_at DESC),
    CONSTRAINT fk_iam_password_history_principal FOREIGN KEY (principal_id) REFERENCES iam_principals(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE iam_password_history;
