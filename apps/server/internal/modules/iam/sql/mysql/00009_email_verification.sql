-- +goose Up
CREATE TABLE iam_email_verification_tokens (
    token_hmac CHAR(64) NOT NULL PRIMARY KEY,
    principal_id VARCHAR(255) NOT NULL,
    email VARCHAR(320) NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    consumed_at BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT fk_iam_email_verification_principal FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE,
    INDEX idx_iam_email_verification_principal (principal_id, consumed_at, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS iam_email_verification_tokens;
